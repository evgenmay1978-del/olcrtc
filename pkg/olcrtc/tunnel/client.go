package tunnel

import (
	"context"
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/access"
	"github.com/openlibrecommunity/olcrtc/internal/client"
)

// ClientConfig configures an embeddable tunnel client: it joins the same
// WebRTC SFU carrier as a [Server], performs the encrypted handshake, and
// exposes a local SOCKS5 proxy that applications point at. It is the
// client-side counterpart to [Config]/[Server] and shares the same carrier,
// crypto key, transport, and cover settings, which must match the server.
type ClientConfig struct {
	// --- carrier selection (must match the server) ---
	Transport string // datachannel, videochannel, seichannel, vp8channel
	Carrier   string // jitsi, telemost, wbstream, none
	RoomURL   string // conference room identifier for the carrier

	// --- direct engine mode (Carrier == "none") ---
	Engine string // livekit, goolom, jitsi
	URL    string
	Token  string

	// --- crypto & networking ---
	KeyHex    string // 64-char hex (32 bytes), must match the server
	LocalAddr string // local SOCKS5 listen address, e.g. "127.0.0.1:8808"
	DNSServer string // optional resolver, e.g. "8.8.8.8:53"

	// --- transport tuning ---
	TransportOptions TransportOptions

	// Cover enables cover-traffic obfuscation. Must match the server's setting.
	// See docs/cover.md.
	Cover CoverConfig

	// AccessToken, when set, is presented to a token-gated server so it can
	// authorize this client (paid/free access). Leave empty for open servers.
	AccessToken string

	// DeviceID overrides the persistent client device identifier. Empty derives
	// one from DeviceIDPath, or generates a random one if both are empty.
	DeviceID string
	// DeviceIDPath persists the auto-generated device ID across restarts.
	DeviceIDPath string
}

// Client is an embeddable tunnel client. Call [Client.Run] to start it.
type Client struct {
	cfg ClientConfig
}

// NewClient returns a Client configured by cfg.
func NewClient(cfg ClientConfig) *Client {
	return &Client{cfg: cfg}
}

// Run starts the client, brings up the carrier link and local SOCKS5 listener,
// and blocks until ctx is cancelled or the carrier ends. Applications connect
// to ClientConfig.LocalAddr as a standard SOCKS5 proxy.
func (c *Client) Run(ctx context.Context) error {
	return c.RunWithReady(ctx, nil)
}

// RunWithReady is like [Client.Run] but invokes onReady once the local SOCKS5
// listener is accepting connections. Useful for tests and orchestration.
func (c *Client) RunWithReady(ctx context.Context, onReady func()) error {
	var claims map[string]any
	if c.cfg.AccessToken != "" {
		claims = map[string]any{access.ClaimToken: c.cfg.AccessToken}
	}
	err := client.RunWithReady(ctx, client.Config{
		Transport:        c.cfg.Transport,
		Carrier:          c.cfg.Carrier,
		RoomURL:          c.cfg.RoomURL,
		Engine:           c.cfg.Engine,
		URL:              c.cfg.URL,
		Token:            c.cfg.Token,
		KeyHex:           c.cfg.KeyHex,
		LocalAddr:        c.cfg.LocalAddr,
		DNSServer:        c.cfg.DNSServer,
		TransportOptions: c.cfg.TransportOptions,
		Cover:            c.cfg.Cover,
		DeviceID:         c.cfg.DeviceID,
		DeviceIDPath:     c.cfg.DeviceIDPath,
		Claims:           claims,
	}, onReady)
	if err != nil {
		return fmt.Errorf("tunnel client: %w", err)
	}
	return nil
}
