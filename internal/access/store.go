package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	// ErrClientNotFound is returned when no registry entry matches a label or token.
	ErrClientNotFound = errors.New("client not found")
	// ErrDuplicateLabel is returned when adding a client whose label already exists.
	ErrDuplicateLabel = errors.New("client label already exists")
)

// Store is a read/write view of the client registry for administrative use
// (the olcrtc-admin tool). Unlike Registry, which is optimized for the hot
// authorization path, Store loads the file, lets the caller mutate the client
// list, and writes it back atomically. It is not safe for concurrent use; the
// admin CLI is single-threaded.
type Store struct {
	path    string
	clients []Client
	now     func() time.Time
}

// OpenStore loads the registry at path for editing. A missing file starts an
// empty store, so the first `add`/`grant` creates it.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now}
	data, err := os.ReadFile(path) //nolint:gosec // admin-supplied registry path
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read access registry %q: %w", path, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse access registry %q: %w", path, err)
	}
	s.clients = f.Clients
	return s, nil
}

// Clients returns a copy of the current client list, sorted by label.
func (s *Store) Clients() []Client {
	out := make([]Client, len(s.clients))
	copy(out, s.clients)
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// Lookup returns a copy of the client with the given label and whether it was
// found.
func (s *Store) Lookup(label string) (Client, bool) {
	i := s.findByLabel(label)
	if i < 0 {
		return Client{}, false
	}
	return s.clients[i], true
}

// findByLabel returns the index of the client with the given label, or -1.
func (s *Store) findByLabel(label string) int {
	for i := range s.clients {
		if s.clients[i].Label == label {
			return i
		}
	}
	return -1
}

// Add inserts a new client and returns the generated token. status selects the
// initial lifecycle state (e.g. StatusActive for a free grant, StatusPending
// for a self-service paid signup awaiting approval). ttl == 0 means no expiry.
func (s *Store) Add(label, contact, status string, ttl time.Duration) (string, error) {
	if s.findByLabel(label) >= 0 {
		return "", fmt.Errorf("%w: %q", ErrDuplicateLabel, label)
	}
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	c := Client{
		Token:   token,
		Label:   label,
		Status:  status,
		Contact: contact,
	}
	if ttl > 0 {
		c.Expires = s.now().Add(ttl).UTC()
	}
	s.clients = append(s.clients, c)
	return token, nil
}

// SetStatus updates a client's lifecycle status by label. When activating with
// a positive ttl, the expiry is (re)set relative to now.
func (s *Store) SetStatus(label, status string, ttl time.Duration) error {
	i := s.findByLabel(label)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	s.clients[i].Status = status
	if status == StatusActive && ttl > 0 {
		s.clients[i].Expires = s.now().Add(ttl).UTC()
	}
	return nil
}

// Rotate generates a fresh token for the client with the given label, leaving
// its status, expiry, and other fields intact. Use it when a token leaks: the
// old token stops working immediately and the new one is returned to reissue.
func (s *Store) Rotate(label string) (string, error) {
	i := s.findByLabel(label)
	if i < 0 {
		return "", fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}
	s.clients[i].Token = token
	return token, nil
}

// SetDisabled toggles revocation by label.
func (s *Store) SetDisabled(label string, disabled bool) error {
	i := s.findByLabel(label)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	s.clients[i].Disabled = disabled
	return nil
}

// AddDevice binds hwid to the client, enforcing MaxDevices. Re-adding an
// already-bound device is a no-op success. Returns ErrDeviceLimit when the cap
// is reached and hwid is new.
func (s *Store) AddDevice(label, hwid string) error {
	i := s.findByLabel(label)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	for _, d := range s.clients[i].Devices {
		if d == hwid {
			return nil
		}
	}
	if len(s.clients[i].Devices) >= MaxDevices {
		return ErrDeviceLimit
	}
	s.clients[i].Devices = append(s.clients[i].Devices, hwid)
	return nil
}

// ResetDevices clears the client's bound devices so the next ones to connect
// re-bind (used when the user changes phones or hits the device cap).
func (s *Store) ResetDevices(label string) error {
	i := s.findByLabel(label)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	s.clients[i].Devices = nil
	return nil
}

// DeviceCount returns how many devices are currently bound to the label.
func (s *Store) DeviceCount(label string) int {
	i := s.findByLabel(label)
	if i < 0 {
		return 0
	}
	return len(s.clients[i].Devices)
}

// Renew activates the client and extends its expiry by ttl, stacking onto any
// remaining time so renewing early does not waste days. Token and bound devices
// are preserved. ttl == 0 means no expiry.
func (s *Store) Renew(label string, ttl time.Duration) error {
	i := s.findByLabel(label)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	s.clients[i].Status = StatusActive
	if ttl <= 0 {
		s.clients[i].Expires = time.Time{}
		return nil
	}
	base := s.now()
	if cur := s.clients[i].Expires; !cur.IsZero() && cur.After(base) {
		base = cur
	}
	s.clients[i].Expires = base.Add(ttl).UTC()
	return nil
}

// MarkPending flags an existing client as pending and records the contact note
// (e.g. the renewal tariff), preserving token, earned expiry, and bound devices.
// Used by the renew flow so a repeat purchase reuses the same client instead of
// creating a duplicate "garbage" entry.
func (s *Store) MarkPending(label, contact string) error {
	i := s.findByLabel(label)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	s.clients[i].Status = StatusPending
	s.clients[i].Contact = contact
	return nil
}

// PruneExpiredPending auto-rejects pending clients whose payment deadline
// (Expires) has passed, so stale unpaid signups don't linger as pending
// forever. It returns the labels that were rejected. A pending client with no
// Expires is left untouched (no deadline set). Renewals are skipped: their
// Expires is the earned subscription end (which may already be past), not a
// payment deadline, so they keep waiting for the operator instead of being
// auto-rejected.
func (s *Store) PruneExpiredPending() []string {
	now := s.now()
	var rejected []string
	for i := range s.clients {
		c := &s.clients[i]
		if c.Status != StatusPending || c.Expires.IsZero() || !now.After(c.Expires) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(c.Contact), RenewContactPrefix) {
			continue
		}
		c.Status = StatusRejected
		rejected = append(rejected, c.Label)
	}
	return rejected
}

// Remove deletes a client by label.
func (s *Store) Remove(label string) error {
	i := s.findByLabel(label)
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrClientNotFound, label)
	}
	s.clients = append(s.clients[:i], s.clients[i+1:]...)
	return nil
}

// Save writes the registry back to disk atomically (temp file + rename) with
// 0600 permissions, so tokens are not world-readable and a crash mid-write
// cannot truncate the live file.
func (s *Store) Save() error {
	for i := range s.clients {
		if s.clients[i].Token == "" {
			return fmt.Errorf("%w: %q", ErrEmptyToken, s.clients[i].Label)
		}
	}
	data, err := json.MarshalIndent(file{Clients: s.clients}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".clients-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp registry: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp registry: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp registry: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename temp registry: %w", err)
	}
	return nil
}
