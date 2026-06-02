package e2e

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/pkg/olcrtc/tunnel"
)

var errEmbeddedAuth = errors.New("embedded auth rejected")

// TestEmbeddableServerClientTunnel exercises the public pkg/olcrtc/tunnel API
// end to end: an embeddable Server and Client, both with cover traffic enabled
// and the client gated by an access token, tunnel real SOCKS5 traffic over the
// in-memory carrier. This pins the embeddable surface olcbox-style apps use.
func TestEmbeddableServerClientTunnel(t *testing.T) {
	carrierName, room := registerMemoryCarrier(t)
	echoAddr := startEchoServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	socksAddr := freeLocalAddr(ctx, t)

	const token = "embedded-token"
	cover := tunnel.CoverConfig{Enabled: true, Interval: 20 * time.Millisecond, Size: 128}

	// Server: gate on a fixed token via a small AuthHook.
	srv := tunnel.New(tunnel.Config{
		Transport: transportData,
		Carrier:   carrierName,
		RoomURL:   testRoom,
		KeyHex:    testKeyHex,
		DNSServer: localDNSServer,
		Cover:     cover,
		AuthHook: func(_ string, claims map[string]any) (string, error) {
			if claims["token"] != token {
				return "", errEmbeddedAuth
			}
			return "embedded-session", nil
		},
	})
	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.Run(ctx) }()
	room.waitConnected(t)

	// Client: present the token and enable matching cover traffic.
	cli := tunnel.NewClient(tunnel.ClientConfig{
		Transport:   transportData,
		Carrier:     carrierName,
		RoomURL:     testRoom,
		KeyHex:      testKeyHex,
		LocalAddr:   socksAddr,
		DNSServer:   localDNSServer,
		Cover:       cover,
		AccessToken: token,
		DeviceID:    testClientDeviceID,
	})
	clientErr := make(chan error, 1)
	go func() { clientErr <- cli.Run(ctx) }()

	conn := eventuallyConnectViaSOCKS(t, socksAddr, echoAddr)
	defer func() { _ = conn.Close() }()

	payload := []byte("embeddable-api-payload\n")
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

	select {
	case err := <-serverErr:
		t.Fatalf("server exited unexpectedly: %v", err)
	case err := <-clientErr:
		t.Fatalf("client exited unexpectedly: %v", err)
	default:
	}
}
