package access

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	tokenValid   = "valid"
	tokenExpired = "expired"
	tokenRevoked = "revoked"
)

func writeRegistry(t *testing.T, clients []Client) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "clients.json")
	data, err := json.Marshal(file{Clients: clients})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestNewRejectsMissingFile(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("New() expected error for missing file, got nil")
	}
}

func TestNewRejectsEmptyToken(t *testing.T) {
	path := writeRegistry(t, []Client{{Token: "", Label: "bad"}})
	if _, err := New(path); !errors.Is(err, ErrEmptyToken) {
		t.Fatalf("New() error = %v, want ErrEmptyToken", err)
	}
}

func TestAuthorize(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	path := writeRegistry(t, []Client{
		{Token: tokenValid, Label: "alice", Expires: future},
		{Token: "forever", Label: "bob"},
		{Token: tokenExpired, Label: "carol", Expires: past},
		{Token: tokenRevoked, Label: "dave", Disabled: true},
	})
	r, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name    string
		claims  map[string]any
		wantErr error
	}{
		{name: "valid", claims: map[string]any{ClaimToken: tokenValid}, wantErr: nil},
		{name: "never expires", claims: map[string]any{ClaimToken: "forever"}, wantErr: nil},
		{name: "expired", claims: map[string]any{ClaimToken: tokenExpired}, wantErr: ErrAccessExpired},
		{name: "revoked", claims: map[string]any{ClaimToken: tokenRevoked}, wantErr: ErrAccessRevoked},
		{name: "unknown", claims: map[string]any{ClaimToken: "nope"}, wantErr: ErrAccessDenied},
		{name: "no token", claims: map[string]any{}, wantErr: ErrNoToken},
		{name: "empty token", claims: map[string]any{ClaimToken: ""}, wantErr: ErrNoToken},
		{name: "wrong type", claims: map[string]any{ClaimToken: 123}, wantErr: ErrNoToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sid, err := r.Authorize("device-1", tt.claims)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Authorize() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Authorize() error = %v, want nil", err)
			}
			if sid == "" {
				t.Fatal("Authorize() returned empty session id")
			}
		})
	}
}

func TestAuthorizeSessionIDsAreUnique(t *testing.T) {
	path := writeRegistry(t, []Client{{Token: "t", Label: "a"}})
	r, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	seen := make(map[string]bool)
	for range 100 {
		sid, err := r.Authorize("d", map[string]any{ClaimToken: "t"})
		if err != nil {
			t.Fatalf("Authorize() error = %v", err)
		}
		if seen[sid] {
			t.Fatalf("duplicate session id %q", sid)
		}
		seen[sid] = true
	}
}

func TestHotReloadRevoke(t *testing.T) {
	path := writeRegistry(t, []Client{{Token: "t", Label: "a"}})
	r, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := r.Authorize("d", map[string]any{ClaimToken: "t"}); err != nil {
		t.Fatalf("Authorize() before revoke error = %v", err)
	}

	// Rewrite the file with the client disabled, advancing mtime so the
	// registry notices the change.
	data, err := json.Marshal(file{Clients: []Client{{Token: "t", Label: "a", Disabled: true}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, err := r.Authorize("d", map[string]any{ClaimToken: "t"}); !errors.Is(err, ErrAccessRevoked) {
		t.Fatalf("Authorize() after revoke error = %v, want ErrAccessRevoked", err)
	}
}

func TestExpiryUsesInjectedClock(t *testing.T) {
	at := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeRegistry(t, []Client{{Token: "t", Expires: at.Add(time.Minute)}})
	r, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	r.now = func() time.Time { return at } // before expiry
	if _, err := r.Authorize("d", map[string]any{ClaimToken: "t"}); err != nil {
		t.Fatalf("Authorize() before expiry error = %v", err)
	}
	r.now = func() time.Time { return at.Add(2 * time.Minute) } // after expiry
	if _, err := r.Authorize("d", map[string]any{ClaimToken: "t"}); !errors.Is(err, ErrAccessExpired) {
		t.Fatalf("Authorize() after expiry error = %v, want ErrAccessExpired", err)
	}
}

func TestGenerateTokenUnique(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if a == b {
		t.Fatal("GenerateToken() returned duplicate tokens")
	}
	if len(a) != tokenBytes*2 {
		t.Fatalf("GenerateToken() len = %d, want %d", len(a), tokenBytes*2)
	}
}

func TestAuthorizeDeviceBinding(t *testing.T) {
	path := writeRegistry(t, []Client{
		{Token: "bound", Label: "b", Status: StatusActive, Devices: []string{"d1", "d2", "d3"}},
		{Token: "free", Label: "f", Status: StatusActive},
	})
	r, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := r.Authorize("d2", map[string]any{ClaimToken: "bound"}); err != nil {
		t.Fatalf("Authorize bound device = %v, want nil", err)
	}
	if _, err := r.Authorize("d4", map[string]any{ClaimToken: "bound"}); !errors.Is(err, ErrDeviceNotAuthorized) {
		t.Fatalf("Authorize unbound device = %v, want ErrDeviceNotAuthorized", err)
	}
	if _, err := r.Authorize("anything", map[string]any{ClaimToken: "free"}); err != nil {
		t.Fatalf("Authorize device-less client = %v, want nil", err)
	}
}
