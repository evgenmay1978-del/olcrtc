package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/access"
	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/muxconn"
	"github.com/openlibrecommunity/olcrtc/internal/server"
)

// writeAccessRegistry creates a clients.json with the given entries and returns
// its path.
func writeAccessRegistry(t *testing.T, clients []access.Client) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clients.json")
	payload := struct {
		Clients []access.Client `json:"clients"`
	}{Clients: clients}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return path
}

// startGatedTunnel brings up a memory-carrier tunnel whose server authorizes
// clients against a token registry and whose client presents accessToken. Both
// ends enable cover traffic. It returns the SOCKS address and the client's
// run error channel so callers can assert success or rejection.
func startGatedTunnel(
	t *testing.T,
	registryPath, accessToken string,
) (string, chan error) {
	t.Helper()

	carrierName, room := registerMemoryCarrier(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	socksAddr := freeLocalAddr(ctx, t)

	reg, err := access.New(registryPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	cover := muxconn.CoverConfig{Enabled: true, Interval: 20 * time.Millisecond, Size: 128}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Run(ctx, server.Config{
			Transport: transportData,
			Carrier:   carrierName,
			RoomURL:   testRoom,
			KeyHex:    testKeyHex,
			DNSServer: localDNSServer,
			Cover:     cover,
			AuthHook:  reg.Authorize,
		})
	}()
	room.waitConnected(t, 1)

	clientErr := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		clientErr <- client.RunWithReady(ctx, client.Config{
			Transport: transportData,
			Carrier:   carrierName,
			RoomURL:   testRoom,
			KeyHex:    testKeyHex,
			DeviceID:  testClientDeviceID,
			LocalAddr: socksAddr,
			DNSServer: localDNSServer,
			Cover:     cover,
			Claims:    map[string]any{access.ClaimToken: accessToken},
		}, func() { close(ready) })
	}()

	return socksAddr, clientErr
}

// TestPaidFlowValidTokenTunnels is the happy path: an active token both
// authorizes the handshake and carries real traffic through the cover-traffic
// framing end to end.
func TestPaidFlowValidTokenTunnels(t *testing.T) {
	token, err := access.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	registry := writeAccessRegistry(t, []access.Client{
		{Token: token, Label: "paid-user", Status: access.StatusActive},
	})
	echoAddr := startEchoServer(t)

	socksAddr, clientErr := startGatedTunnel(t, registry, token)

	conn := eventuallyConnectViaSOCKS(t, socksAddr, echoAddr)
	defer func() { _ = conn.Close() }()

	payload := []byte("paid-flow-payload\n")
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
	case err := <-clientErr:
		t.Fatalf("client exited unexpectedly: %v", err)
	default:
	}
}

// TestPaidFlowRevokedTokenIsRejected confirms a disabled client cannot tunnel:
// the server rejects the handshake, so no SOCKS connection ever succeeds.
func TestPaidFlowRevokedTokenIsRejected(t *testing.T) {
	token, err := access.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	registry := writeAccessRegistry(t, []access.Client{
		{Token: token, Label: "revoked-user", Status: access.StatusActive, Disabled: true},
	})
	echoAddr := startEchoServer(t)

	socksAddr, _ := startGatedTunnel(t, registry, token)

	// A revoked token must never yield a working tunnel: the server rejects
	// the handshake, so the SOCKS CONNECT (which needs an end-to-end tunnel
	// stream) cannot complete within the window.
	if conn, err := connectViaSOCKSWithin(socksAddr, echoAddr, 3*time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("revoked token produced a working tunnel; want rejection")
	}
}
