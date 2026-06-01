// Package access provides a file-backed client registry for authorizing paid
// olcRTC clients. It plugs into the server via an AuthFunc-compatible hook:
// each client presents an opaque token in its handshake claims, and the
// registry decides whether to admit it based on expiry and revocation status.
//
// The registry file is re-read when its modification time changes, so access
// can be granted or revoked without restarting the server. If a reload fails,
// the last known-good snapshot keeps serving.
package access

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// claimToken is the claims key a client uses to present its access token.
const claimToken = "token"

// tokenBytes is the number of random bytes in a generated token (256-bit).
const tokenBytes = 32

// Subscription lifecycle statuses. An empty status is treated as StatusActive
// for backward compatibility with registries written before this field existed.
const (
	// StatusPending means the client claims to have paid; awaiting admin review.
	StatusPending = "pending"
	// StatusActive means access is granted (paid-and-approved, or free grant).
	StatusActive = "active"
	// StatusRejected means the admin declined the payment; no access.
	StatusRejected = "rejected"
)

var (
	// ErrAccessDenied is returned when no client matches the presented token.
	// The message is deliberately generic so it can be forwarded to the client
	// without revealing whether a token exists.
	ErrAccessDenied = errors.New("access denied")
	// ErrAccessExpired is returned when a matching client's subscription lapsed.
	ErrAccessExpired = errors.New("access expired")
	// ErrAccessRevoked is returned when a matching client has been disabled.
	ErrAccessRevoked = errors.New("access revoked")
	// ErrAccessPending is returned when a matching client has not been approved yet.
	ErrAccessPending = errors.New("access pending payment approval")
	// ErrNoToken is returned when the client presented no token in its claims.
	ErrNoToken = errors.New("no access token presented")
	// ErrEmptyToken is returned when a registry entry has an empty token.
	ErrEmptyToken = errors.New("client token must not be empty")
)

// Client is one subscriber's access record.
type Client struct {
	// Token is the opaque secret the client presents in claims["token"].
	Token string `json:"token"`
	// Label is a human-readable name for the client (e.g. who bought access).
	Label string `json:"label,omitempty"`
	// Status is the subscription lifecycle state (pending/active/rejected).
	// Empty is treated as active for backward compatibility.
	Status string `json:"status,omitempty"`
	// Expires is the subscription end time. The zero value means "never expires".
	Expires time.Time `json:"expires,omitempty"`
	// Disabled, when true, revokes the client regardless of status or expiry.
	Disabled bool `json:"disabled,omitempty"`
	// Contact is an optional free-form note (e.g. phone last digits the client
	// paid from) to help the admin match a payment to this client.
	Contact string `json:"contact,omitempty"`
}

// isActive reports whether the client should currently be admitted. An empty
// status counts as active so legacy registries keep working.
func (c *Client) isActive() bool {
	return c.Status == "" || c.Status == StatusActive
}

// file is the on-disk registry schema.
type file struct {
	Clients []Client `json:"clients"`
}

// Registry authorizes clients from a JSON file, hot-reloading on mtime change.
// It is safe for concurrent use.
type Registry struct {
	path string
	now  func() time.Time // injectable clock for tests

	mu      sync.RWMutex
	clients []Client
	modTime time.Time
}

// New loads the registry from path and returns it. The file must exist and
// parse; every client entry must have a non-empty token.
func New(path string) (*Registry, error) {
	r := &Registry{path: path, now: time.Now}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// reload reads and validates the registry file, replacing the in-memory
// snapshot. Callers hold no locks; reload takes the write lock itself.
func (r *Registry) reload() error {
	info, err := os.Stat(r.path)
	if err != nil {
		return fmt.Errorf("stat access registry %q: %w", r.path, err)
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return fmt.Errorf("read access registry %q: %w", r.path, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse access registry %q: %w", r.path, err)
	}
	for i := range f.Clients {
		if f.Clients[i].Token == "" {
			return fmt.Errorf("%w: entry %d (%q)", ErrEmptyToken, i, f.Clients[i].Label)
		}
	}

	r.mu.Lock()
	r.clients = f.Clients
	r.modTime = info.ModTime()
	r.mu.Unlock()
	return nil
}

// maybeReload re-reads the file if its mtime changed since the last load. A
// failed reload is non-fatal: the previous snapshot keeps serving.
func (r *Registry) maybeReload() {
	info, err := os.Stat(r.path)
	if err != nil {
		return
	}
	r.mu.RLock()
	unchanged := info.ModTime().Equal(r.modTime)
	r.mu.RUnlock()
	if unchanged {
		return
	}
	_ = r.reload()
}

// Authorize implements the server's AuthFunc contract. It extracts the token
// from claims, matches it against the registry in constant time, and admits
// the client unless it is unknown, expired, or revoked. On success it returns
// a fresh random session ID.
func (r *Registry) Authorize(deviceID string, claims map[string]any) (string, error) {
	_ = deviceID // identity is carried by the token, not the device ID

	token, ok := claims[claimToken].(string)
	if !ok || token == "" {
		return "", ErrNoToken
	}

	r.maybeReload()

	r.mu.RLock()
	clients := r.clients
	r.mu.RUnlock()

	var matched *Client
	for i := range clients {
		if subtle.ConstantTimeCompare([]byte(clients[i].Token), []byte(token)) == 1 {
			matched = &clients[i]
			break
		}
	}
	if matched == nil {
		return "", ErrAccessDenied
	}
	if err := r.checkAdmissible(matched); err != nil {
		return "", err
	}
	return newSessionID()
}

// checkAdmissible returns nil if the matched client may currently be admitted,
// or the reason it must be rejected (revoked, pending, expired, or inactive).
func (r *Registry) checkAdmissible(c *Client) error {
	switch {
	case c.Disabled:
		return ErrAccessRevoked
	case c.Status == StatusPending:
		return ErrAccessPending
	case !c.isActive():
		return ErrAccessDenied
	case !c.Expires.IsZero() && r.now().After(c.Expires):
		return ErrAccessExpired
	default:
		return nil
	}
}

// newSessionID returns a random 128-bit hex session identifier.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// GenerateToken returns a new cryptographically random access token, hex
// encoded, suitable for issuing to a new client.
func GenerateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
