package e2e

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/app/session"
	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/engine"
	enginebuiltin "github.com/openlibrecommunity/olcrtc/internal/engine/builtin"
	"github.com/openlibrecommunity/olcrtc/internal/server"
)

var errFlakyConnect = errors.New("flaky carrier: first attempts fail")

// joinMemoryRoom adds a stream backed by room and returns it as an engine
// session.
func joinMemoryRoom(room *memoryRoom, cfg enginebuiltin.Config) engine.Session {
	stream := newMemoryStream(room, cfg.OnData)
	room.mu.Lock()
	room.streams[stream] = struct{}{}
	room.mu.Unlock()
	return stream
}

// registerFlakyClientCarriers registers two carriers sharing one in-memory
// room: a reliable one for the server and a flaky one for the client whose
// first failCount connection attempts fail. This models a public SFU that does
// not accept the client's very first connect (common on mobile networks) so we
// can prove the client retries the initial connection while the server stays up.
func registerFlakyClientCarriers(t *testing.T, failCount int32) (string, string) {
	t.Helper()
	session.RegisterDefaults()

	room := &memoryRoom{streams: make(map[*memoryStream]struct{})}
	serverCarrier := "e2e-flaky-srv-" + t.Name()
	clientCarrier := "e2e-flaky-cli-" + t.Name()

	enginebuiltin.Register(serverCarrier, func(_ context.Context, cfg enginebuiltin.Config) (engine.Session, error) {
		return joinMemoryRoom(room, cfg), nil
	})
	var attempts atomic.Int32
	enginebuiltin.Register(clientCarrier, func(_ context.Context, cfg enginebuiltin.Config) (engine.Session, error) {
		if attempts.Add(1) <= failCount {
			return nil, errFlakyConnect
		}
		return joinMemoryRoom(room, cfg), nil
	})
	return serverCarrier, clientCarrier
}

// TestClientRetriesInitialConnect verifies the client keeps retrying the first
// connection until the carrier accepts it, then tunnels real traffic. Without
// the retry the client would give up on the first failure.
func TestClientRetriesInitialConnect(t *testing.T) {
	// Speed up the initial-connect backoff for the test.
	client.SetInitialConnectBackoffForTest(20*time.Millisecond, 100*time.Millisecond)
	t.Cleanup(func() { client.SetInitialConnectBackoffForTest(2*time.Second, 30*time.Second) })

	serverCarrier, clientCarrier := registerFlakyClientCarriers(t, 2) // client fails twice, then succeeds
	echoAddr := startEchoServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	socksAddr := freeLocalAddr(ctx, t)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Run(ctx, server.Config{
			Transport: transportData,
			Carrier:   serverCarrier,
			RoomURL:   testRoom,
			KeyHex:    testKeyHex,
			DNSServer: localDNSServer,
		})
	}()

	ready := make(chan struct{})
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- client.RunWithReady(ctx, client.Config{
			Transport: transportData,
			Carrier:   clientCarrier,
			RoomURL:   testRoom,
			KeyHex:    testKeyHex,
			DeviceID:            testClientDeviceID,
			LocalAddr:           socksAddr,
			DNSServer:           localDNSServer,
			RetryInitialConnect: true,
		}, func() { close(ready) })
	}()

	// The listener only comes up after the retried connection succeeds.
	waitForReady(t, ready)

	conn := eventuallyConnectViaSOCKS(t, socksAddr, echoAddr)
	defer func() { _ = conn.Close() }()
	payload := []byte("retry-then-tunnel\n")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write tunneled payload: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read tunneled echo: %v", err)
	}
	if !bytes.Equal(line, payload) {
		t.Fatalf("echo = %q, want %q", line, payload)
	}
}
