package access

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAddSaveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	token, err := s.Add("alice", "tinkoff 6564", StatusActive, time.Hour)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if token == "" {
		t.Fatal("Add returned empty token")
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The saved registry must authorize the new token.
	r, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := r.Authorize("d", map[string]any{ClaimToken: token}); err != nil {
		t.Fatalf("Authorize active client: %v", err)
	}
}

func TestStoreDuplicateLabel(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "c.json"))
	if _, err := s.Add("bob", "", StatusActive, 0); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if _, err := s.Add("bob", "", StatusActive, 0); !errors.Is(err, ErrDuplicateLabel) {
		t.Fatalf("duplicate Add error = %v, want ErrDuplicateLabel", err)
	}
}

func TestStorePendingApproveRejectFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	s, _ := OpenStore(path)
	token, err := s.Add("carol", "sber 6564", StatusPending, 0)
	if err != nil {
		t.Fatalf("Add pending: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Pending must NOT be admitted.
	r, _ := New(path)
	if _, err := r.Authorize("d", map[string]any{ClaimToken: token}); !errors.Is(err, ErrAccessPending) {
		t.Fatalf("pending Authorize error = %v, want ErrAccessPending", err)
	}

	// Approve -> active.
	s2, _ := OpenStore(path)
	if err := s2.SetStatus("carol", StatusActive, 24*time.Hour); err != nil {
		t.Fatalf("SetStatus active: %v", err)
	}
	if err := s2.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r2, _ := New(path)
	if _, err := r2.Authorize("d", map[string]any{ClaimToken: token}); err != nil {
		t.Fatalf("approved Authorize: %v", err)
	}

	// Reject -> denied.
	s3, _ := OpenStore(path)
	if err := s3.SetStatus("carol", StatusRejected, 0); err != nil {
		t.Fatalf("SetStatus rejected: %v", err)
	}
	if err := s3.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	r3, _ := New(path)
	if _, err := r3.Authorize("d", map[string]any{ClaimToken: token}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("rejected Authorize error = %v, want ErrAccessDenied", err)
	}
}

func TestStoreRemoveAndDisable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	s, _ := OpenStore(path)
	tok, _ := s.Add("dave", "", StatusActive, 0)
	if err := s.SetDisabled("dave", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	_ = s.Save()
	r, _ := New(path)
	if _, err := r.Authorize("d", map[string]any{ClaimToken: tok}); !errors.Is(err, ErrAccessRevoked) {
		t.Fatalf("disabled Authorize error = %v, want ErrAccessRevoked", err)
	}

	s2, _ := OpenStore(path)
	if err := s2.Remove("dave"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(s2.Clients()) != 0 {
		t.Fatalf("after Remove, clients = %d, want 0", len(s2.Clients()))
	}
	if err := s2.Remove("ghost"); !errors.Is(err, ErrClientNotFound) {
		t.Fatalf("Remove missing error = %v, want ErrClientNotFound", err)
	}
}

func TestPruneExpiredPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	s, _ := OpenStore(path)
	s.now = func() time.Time { return base }

	// Pending with a 1h deadline, plus a far-future one, a no-deadline one,
	// and an active client.
	tokExpired, _ := s.Add("late", "", StatusPending, time.Hour)
	tokFuture, _ := s.Add("ontime", "", StatusPending, 100*time.Hour)
	_, _ = s.Add("nodeadline", "", StatusPending, 0)
	_, _ = s.Add("paid", "", StatusActive, 0)

	// Advance the clock past the first deadline but not the second.
	s.now = func() time.Time { return base.Add(2 * time.Hour) }
	rejected := s.PruneExpiredPending()
	if len(rejected) != 1 || rejected[0] != "late" {
		t.Fatalf("pruned = %v, want [late]", rejected)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	r, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Pruned one is now rejected -> denied.
	if _, err := r.Authorize("d", map[string]any{ClaimToken: tokExpired}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("pruned client error = %v, want ErrAccessDenied", err)
	}
	// Future-deadline one is still pending.
	if _, err := r.Authorize("d", map[string]any{ClaimToken: tokFuture}); !errors.Is(err, ErrAccessPending) {
		t.Fatalf("future pending error = %v, want ErrAccessPending", err)
	}
}

func TestRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	future := time.Now().Add(24 * time.Hour)
	s, _ := OpenStore(path)
	oldToken, _ := s.Add("eve", "", StatusActive, 0)
	// Give it an explicit expiry so we can confirm rotation preserves it.
	_ = s.SetStatus("eve", StatusActive, 0)
	s.clients[0].Expires = future

	newToken, err := s.Rotate("eve")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newToken == oldToken || newToken == "" {
		t.Fatalf("Rotate returned %q (old %q)", newToken, oldToken)
	}
	if !s.clients[0].Expires.Equal(future) {
		t.Fatalf("Rotate changed expiry: %v != %v", s.clients[0].Expires, future)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	r, _ := New(path)
	// Old token rejected, new token authorized.
	if _, err := r.Authorize("d", map[string]any{ClaimToken: oldToken}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("old token error = %v, want ErrAccessDenied", err)
	}
	if _, err := r.Authorize("d", map[string]any{ClaimToken: newToken}); err != nil {
		t.Fatalf("new token error = %v, want nil", err)
	}

	// Rotating a missing client errors.
	if _, err := s.Rotate("ghost"); !errors.Is(err, ErrClientNotFound) {
		t.Fatalf("Rotate(ghost) = %v, want ErrClientNotFound", err)
	}
}

func TestClientsSortedByLabel(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "c.json"))
	_, _ = s.Add("zoe", "", StatusActive, 0)
	_, _ = s.Add("amy", "", StatusActive, 0)
	got := s.Clients()
	if got[0].Label != "amy" || got[1].Label != "zoe" {
		t.Fatalf("Clients not sorted: %q, %q", got[0].Label, got[1].Label)
	}
}
