// Package muxconn adapts a link.Link into an io.ReadWriteCloser suitable for
// driving a smux session. The wrapper applies AEAD on every wire-bound write
// and inverts it on every received message before exposing the bytes as a
// byte stream.
//
// Link semantics are message-oriented: each Send produces exactly one OnData
// on the peer. smux operates on a pure byte stream (header + payload may be
// glued or split across reads). We bridge by:
//
//   - Treating each Push as an opaque chunk handed off via a channel that
//     Read drains in arbitrary slices, retaining any tail bytes that did
//     not fit the caller's buffer for the next Read.
//   - Letting smux's sendLoop call Write once per frame; we encrypt and hand
//     the whole buffer to the link as a single message. Length boundaries
//     are preserved end-to-end by the transport (KCP length-prefix framing
//     in vp8channel, native message boundaries in datachannel).
package muxconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
)

// ErrClosed is returned from Read/Write after the conn has been closed.
var ErrClosed = errors.New("muxconn: closed")

// Frame type bytes, prepended to the plaintext before AEAD encryption when
// framing is enabled. Because the type lives inside the ciphertext it is
// invisible to an on-path observer; padding frames are indistinguishable from
// real ones by size or content.
const (
	frameReal byte = 0x00 // carries real tunnel data
	framePad  byte = 0x01 // cover traffic, dropped by the receiver
)

const (
	// inboundQueue is the buffered capacity of the Push -> Read pipeline.
	// It absorbs short Read stalls without applying back-pressure to the
	// transport callback. Frames are typically smux-sized (well under
	// 16 KiB), so 256 amounts to a few MiB of in-flight data, which is
	// enough for sustained throughput on every transport we have without
	// unbounded growth on a stuck reader.
	inboundQueue = 256

	// pooledFrameCap is the capacity each pooled plaintext buffer is born
	// with. It is sized to fit the largest smux frame any of our
	// transports will deliver after AEAD overhead is stripped (datachannel
	// caps at 12 KiB on the wire, vp8channel at 60 KiB; we round up to
	// give Open room to write in place without growing the slice).
	pooledFrameCap = 64 * 1024
)

// frameBufPool recycles plaintext buffers between Push (decrypts a wire
// frame into a buffer) and Read (consumes the buffer fully then returns
// it). It is global so all Conn instances share the same hot cache -
// most clients in the same process talk to a handful of peers, and
// per-Conn pools fragment the warm set unnecessarily.
var frameBufPool = sync.Pool{ //nolint:gochecknoglobals // intentional process-wide buffer pool
	New: func() any {
		b := make([]byte, 0, pooledFrameCap)
		return &b
	},
}

func acquireFrameBuf() *[]byte {
	bp := frameBufPool.Get().(*[]byte) //nolint:forcetypeassert // pool only ever holds *[]byte
	*bp = (*bp)[:0]
	return bp
}

func releaseFrameBuf(bp *[]byte) {
	if bp == nil {
		return
	}
	// Drop oversized buffers so a one-off huge frame can't poison the
	// pool's working set forever.
	if cap(*bp) > pooledFrameCap*2 {
		return
	}
	*bp = (*bp)[:0]
	frameBufPool.Put(bp)
}

// Conn is an io.ReadWriteCloser over a [transport.Transport] with optional AEAD wrapping.
//
// Push produces decrypted plaintext frames into an internal channel; Read
// drains the channel and slices each frame across as many caller buffers
// as needed. The hot path is lock-free: a single producer (the transport
// callback) and a single consumer (smux's read loop) communicate via a
// buffered channel without any cond/mutex ping-pong.
//
// Plaintext buffers are recycled through frameBufPool: Push borrows a
// buffer to decrypt into, ships it through the channel, and Read returns
// the buffer to the pool once its caller has consumed all the bytes.
type Conn struct {
	ln     transport.Transport
	send   func([]byte) error
	cipher *crypto.Cipher

	in        chan *[]byte
	closeOnce sync.Once
	closeCh   chan struct{}
	closed    atomic.Bool

	// leftoverBuf holds the pool buffer whose tail is still in
	// `leftover`. When `leftover` empties we return leftoverBuf to the
	// pool and clear both fields. Touched only by Read.
	leftoverBuf *[]byte
	leftover    []byte

	// framed enables one-byte frame typing (real vs padding) inside the
	// AEAD. It must be set identically on both peers before the conn is
	// used; when false the wire format is byte-for-byte the legacy format.
	framed bool
	// sendMu serializes link sends so the cover-traffic pacer and Write
	// never interleave encrypted frames on the wire. Only used when framed.
	sendMu sync.Mutex
	// lastSendNanos is the UnixNano of the last real (non-padding) send,
	// used by the cover pacer to stay quiet while real data is flowing.
	lastSendNanos atomic.Int64
}

// New wires a Conn over the given transport. Push must be set as the
// transport's OnData callback before this conn is used.
func New(ln transport.Transport, cipher *crypto.Cipher) *Conn {
	return &Conn{
		ln:      ln,
		send:    ln.Send,
		cipher:  cipher,
		in:      make(chan *[]byte, inboundQueue),
		closeCh: make(chan struct{}),
	}
}

// NewPeer wires a Conn whose writes are addressed to a specific transport peer.
func NewPeer(ln transport.PeerTransport, cipher *crypto.Cipher, peerID string) *Conn {
	return &Conn{
		ln: ln,
		send: func(data []byte) error {
			return ln.SendTo(peerID, data)
		},
		cipher:  cipher,
		in:      make(chan *[]byte, inboundQueue),
		closeCh: make(chan struct{}),
	}
}

// Push hands an encrypted wire payload (one OnData event) to the conn.
//
// On the producer side: borrow a pooled plaintext buffer, decrypt into
// it, then either deliver via the inbound channel or, if the caller has
// Close'd, return the buffer to the pool. Blocking forever on a wedged
// reader would wedge the transport callback and trip its watchdog, so we
// also bail on closeCh.
func (c *Conn) Push(ciphertext []byte) {
	bufPtr := acquireFrameBuf()
	pt, err := c.cipher.DecryptInto(*bufPtr, ciphertext)
	if err != nil {
		releaseFrameBuf(bufPtr)
		logger.Debugf("muxconn: decrypt failed, dropping frame: %v", err)
		return
	}
	if c.framed {
		stripped, deliver := stripFrameType(pt)
		if !deliver {
			releaseFrameBuf(bufPtr) // padding or malformed: never reaches Read
			return
		}
		pt = stripped
	}
	*bufPtr = pt
	if c.closed.Load() {
		releaseFrameBuf(bufPtr)
		return
	}
	select {
	case c.in <- bufPtr:
	case <-c.closeCh:
		releaseFrameBuf(bufPtr)
	}
}

// Read implements io.Reader. Blocks until at least one byte is available;
// after that, drains additional ready frames non-blockingly to fill p, so
// a single Read can absorb several queued frames in one go. This matches
// the prior cond/append-based implementation's concatenation behaviour
// and lets smux's bufio reader pull large chunks at a time.
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.leftover) == 0 {
		bufPtr, ok := c.takeFrame()
		if !ok {
			return 0, io.EOF
		}
		c.leftoverBuf = bufPtr
		c.leftover = *bufPtr
	}
	n := copy(p, c.leftover)
	c.leftover = c.leftover[n:]
	c.recycleIfDrained()

	// Greedily pull additional frames already sitting in the queue,
	// without blocking. This keeps the channel from accumulating a
	// backlog when the consumer asks for a large buffer.
	for n < len(p) && len(c.leftover) == 0 {
		select {
		case bufPtr, ok := <-c.in:
			if !ok {
				return n, nil
			}
			data := *bufPtr
			m := copy(p[n:], data)
			n += m
			if m < len(data) {
				c.leftoverBuf = bufPtr
				c.leftover = data[m:]
			} else {
				releaseFrameBuf(bufPtr)
			}
		default:
			return n, nil
		}
	}
	return n, nil
}

// takeFrame blocks until a frame is available or the conn is closed.
// On a clean close it still drains any frame that landed before the
// close signal won the race, so a peer that shuts us down right after a
// final write doesn't lose data.
func (c *Conn) takeFrame() (*[]byte, bool) {
	select {
	case bufPtr, ok := <-c.in:
		if !ok {
			return nil, false
		}
		return bufPtr, true
	case <-c.closeCh:
		select {
		case bufPtr, ok := <-c.in:
			if !ok {
				return nil, false
			}
			return bufPtr, true
		default:
			return nil, false
		}
	}
}

func (c *Conn) recycleIfDrained() {
	if len(c.leftover) == 0 && c.leftoverBuf != nil {
		releaseFrameBuf(c.leftoverBuf)
		c.leftoverBuf = nil
	}
}

// Write encrypts p and ships it to the link as a single message. Blocks while
// the link signals back-pressure.
func (c *Conn) Write(p []byte) (int, error) {
	// Spin briefly first - on a healthy link CanSend usually clears within
	// well under a millisecond, so a 10ms sleep adds visible per-frame
	// latency to interactive request/response traffic. Fall back to a
	// modest sleep only if the link is truly congested.
	const (
		fastSpinAttempts = 200
		slowPollDelay    = 2 * time.Millisecond
	)
	for attempt := 0; ; attempt++ {
		if c.closed.Load() {
			return 0, ErrClosed
		}
		if c.ln.CanSend() {
			break
		}
		if attempt < fastSpinAttempts {
			runtime.Gosched()
			continue
		}
		time.Sleep(slowPollDelay)
	}

	if c.framed {
		if err := c.sendFrame(frameReal, p); err != nil {
			return 0, err
		}
		c.lastSendNanos.Store(time.Now().UnixNano())
		return len(p), nil
	}

	enc, err := c.cipher.Encrypt(p)
	if err != nil {
		return 0, fmt.Errorf("encrypt: %w", err)
	}
	if err := c.send(enc); err != nil {
		return 0, fmt.Errorf("send: %w", err)
	}
	return len(p), nil
}

// Close unblocks any pending Read with io.EOF.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.closeCh)
	})
	return nil
}

// EnableFraming turns on one-byte frame typing. It must be called on both
// peers, before the conn carries traffic, otherwise the receiver will
// misinterpret the wire format. With framing off the conn is byte-for-byte
// compatible with the legacy format.
func (c *Conn) EnableFraming() { c.framed = true }

// CoverConfig controls cover-traffic obfuscation. The zero value (Enabled
// false) leaves the conn in the legacy, byte-for-byte-compatible mode.
type CoverConfig struct {
	// Enabled turns on frame typing on this conn. Both peers must agree.
	Enabled bool
	// Interval is the idle gap after which a padding frame is emitted. Zero
	// enables framing without an active pacer (padding only rides along).
	Interval time.Duration
	// Size is the padding payload size in bytes.
	Size int
}

// ApplyCover enables framing and, if configured, starts the idle cover pacer.
// It is the single entry point used to wire obfuscation onto a freshly built
// conn from both the client and server sides, keeping the two ends symmetric.
func (c *Conn) ApplyCover(ctx context.Context, cfg CoverConfig) {
	if !cfg.Enabled {
		return
	}
	c.EnableFraming()
	c.StartCover(ctx, cfg.Interval, cfg.Size)
}

// stripFrameType reads the leading type byte from a decrypted frame and
// returns the remaining payload together with whether it should reach Read.
// Padding and malformed frames return deliver=false. The payload is shifted
// left in place so the pooled buffer keeps its zero offset (and full capacity).
func stripFrameType(pt []byte) ([]byte, bool) {
	if len(pt) == 0 {
		return nil, false
	}
	typ := pt[0]
	n := copy(pt, pt[1:]) // left-shift; copy handles the overlap (memmove)
	pt = pt[:n]
	if typ != frameReal {
		return nil, false // framePad or unknown: drop
	}
	return pt, true
}

// sendFrame encrypts typ||p and writes it to the link as one message. The
// send is serialized so the cover pacer and Write never interleave on the wire.
func (c *Conn) sendFrame(typ byte, p []byte) error {
	buf := make([]byte, 1+len(p))
	buf[0] = typ
	copy(buf[1:], p)
	enc, err := c.cipher.Encrypt(buf)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := c.send(enc); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

// StartCover launches a background pacer that emits padding frames whenever the
// link has been idle for at least interval, so the traffic envelope stays
// "always on" like a real video call instead of going silent between bursts.
// It is a no-op unless framing is enabled and interval > 0. The pacer stops
// when ctx is cancelled or the conn is closed.
func (c *Conn) StartCover(ctx context.Context, interval time.Duration, size int) {
	if !c.framed || interval <= 0 {
		return
	}
	if size <= 0 {
		size = 1
	}
	go c.coverLoop(ctx, interval, size)
}

func (c *Conn) coverLoop(ctx context.Context, interval time.Duration, size int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closeCh:
			return
		case <-ticker.C:
			if c.closed.Load() {
				return
			}
			// Stay quiet while real data is flowing: only pad genuine gaps.
			idle := time.Since(time.Unix(0, c.lastSendNanos.Load()))
			if idle < interval || !c.ln.CanSend() {
				continue
			}
			// Zero payload is fine: the AEAD makes every ciphertext look
			// random regardless of plaintext, so padding is indistinguishable.
			pad := make([]byte, size)
			if err := c.sendFrame(framePad, pad); err != nil {
				logger.Debugf("muxconn: cover send failed: %v", err)
			}
		}
	}
}
