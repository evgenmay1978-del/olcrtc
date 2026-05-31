package muxconn

import (
	"context"
	"testing"
	"time"
)

// sentFrames returns a copy of everything the stub link has sent so far.
func sentFrames(s *stubLink) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.sent))
	copy(out, s.sent)
	return out
}

func TestUnframedWireUnchanged(t *testing.T) {
	ln := &stubLink{canSend: true}
	cipher := newTestCipher(t)
	c := New(ln, cipher)

	payload := []byte("hello world")
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sent := sentFrames(ln)
	if len(sent) != 1 {
		t.Fatalf("sent %d frames, want 1", len(sent))
	}
	// Legacy format: decrypted bytes equal the payload, no type prefix.
	got, err := cipher.Decrypt(sent[0])
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("decrypted %q, want %q", got, payload)
	}
}

func TestFramedRoundTrip(t *testing.T) {
	ln := &stubLink{canSend: true}
	cipher := newTestCipher(t)

	sender := New(ln, cipher)
	sender.EnableFraming()
	receiver := New(ln, cipher)
	receiver.EnableFraming()

	payload := []byte("framed payload")
	if _, err := sender.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sent := sentFrames(ln)
	if len(sent) != 1 {
		t.Fatalf("sent %d frames, want 1", len(sent))
	}
	// The on-wire plaintext carries a leading frameReal byte.
	pt, err := cipher.Decrypt(sent[0])
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if len(pt) != len(payload)+1 || pt[0] != frameReal {
		t.Fatalf("framed plaintext = %v (type %d), want real+payload", pt, pt[0])
	}

	receiver.Push(sent[0])
	buf := make([]byte, 64)
	n, err := receiver.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Fatalf("Read %q, want %q", buf[:n], payload)
	}
}

func TestFramedDropsPadding(t *testing.T) {
	ln := &stubLink{canSend: true}
	cipher := newTestCipher(t)

	sender := New(ln, cipher)
	sender.EnableFraming()
	receiver := New(ln, cipher)
	receiver.EnableFraming()

	// One padding frame followed by one real frame.
	if err := sender.sendFrame(framePad, []byte("xxxxxxxx")); err != nil {
		t.Fatalf("sendFrame pad: %v", err)
	}
	realData := []byte("real data")
	if _, err := sender.Write(realData); err != nil {
		t.Fatalf("Write: %v", err)
	}
	sent := sentFrames(ln)
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(sent))
	}

	// The receiver must drop the pad and surface only the real payload.
	receiver.Push(sent[0]) // pad
	receiver.Push(sent[1]) // real
	buf := make([]byte, 64)
	n, err := receiver.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != string(realData) {
		t.Fatalf("Read %q, want %q (pad leaked?)", buf[:n], realData)
	}
}

func TestCoverLoopEmitsWhenIdle(t *testing.T) {
	ln := &stubLink{canSend: true}
	cipher := newTestCipher(t)
	c := New(ln, cipher)
	c.EnableFraming()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.StartCover(ctx, 10*time.Millisecond, 32)

	// Wait for the idle pacer to emit at least one cover frame.
	deadline := time.After(2 * time.Second)
	for len(sentFrames(ln)) == 0 {
		select {
		case <-deadline:
			t.Fatal("cover pacer emitted no frames while idle")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()

	// Every emitted frame must be padding.
	for i, enc := range sentFrames(ln) {
		pt, err := cipher.Decrypt(enc)
		if err != nil {
			t.Fatalf("decrypt frame %d: %v", i, err)
		}
		if len(pt) == 0 || pt[0] != framePad {
			t.Fatalf("frame %d is not padding (type %d)", i, pt[0])
		}
	}
}

func TestCoverLoopNoopWhenUnframed(t *testing.T) {
	ln := &stubLink{canSend: true}
	cipher := newTestCipher(t)
	c := New(ln, cipher)
	// Framing not enabled: StartCover must be a no-op.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.StartCover(ctx, 5*time.Millisecond, 16)
	time.Sleep(50 * time.Millisecond)

	if got := len(sentFrames(ln)); got != 0 {
		t.Fatalf("unframed cover emitted %d frames, want 0", got)
	}
}
