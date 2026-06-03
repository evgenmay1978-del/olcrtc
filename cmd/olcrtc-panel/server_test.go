package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/access"
)

const (
	labelAlice = "alice"
	fieldDays  = "days"
	fieldLabel = "label"
)

func newTestServer(t *testing.T) (*server, string) {
	t.Helper()
	reg := filepath.Join(t.TempDir(), "clients.json")
	return newServer(reg, "admin", "secret"), reg
}

// doPost issues an authenticated POST request against the panel handler.
func doPost(t *testing.T, s *server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func TestPanelRequiresAuth(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-auth status = %d, want 401", rec.Code)
	}
	// Wrong password also rejected.
	req2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req2.SetBasicAuth("admin", "wrong")
	rec2 := httptest.NewRecorder()
	s.routes().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("bad-pass status = %d, want 401", rec2.Code)
	}
}

func TestPanelCreateAndListAndDelete(t *testing.T) {
	s, reg := newTestServer(t)

	rec := doPost(t, s, "/create", url.Values{
		fieldLabel: {labelAlice}, fieldDays: {"30"}, "contact": {"tel"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Token (copy now):") {
		t.Fatal("create did not surface a token")
	}

	// The registry the server reads must now authorize the created client.
	r, err := access.New(reg)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	store, _ := access.OpenStore(reg)
	c, ok := store.Lookup(labelAlice)
	if !ok {
		t.Fatal("alice not in registry")
	}
	if _, err := r.Authorize("d", map[string]any{access.ClaimToken: c.Token}); err != nil {
		t.Fatalf("created client not authorized: %v", err)
	}

	// Disable -> revoked.
	if rec := doPost(t, s, "/disable", url.Values{fieldLabel: {labelAlice}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("disable status = %d, want 303", rec.Code)
	}
	r2, _ := access.New(reg)
	if _, err := r2.Authorize("d", map[string]any{access.ClaimToken: c.Token}); err == nil {
		t.Fatal("disabled client should not authorize")
	}

	// Delete -> empty.
	if rec := doPost(t, s, "/delete", url.Values{fieldLabel: {labelAlice}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want 303", rec.Code)
	}
	store2, _ := access.OpenStore(reg)
	if len(store2.Clients()) != 0 {
		t.Fatalf("registry not empty after delete: %d", len(store2.Clients()))
	}
}

func TestPanelRotateChangesToken(t *testing.T) {
	s, reg := newTestServer(t)
	_ = doPost(t, s, "/create", url.Values{fieldLabel: {"bob"}, fieldDays: {"0"}})
	store, _ := access.OpenStore(reg)
	before, _ := store.Lookup("bob")

	rec := doPost(t, s, "/rotate", url.Values{fieldLabel: {"bob"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "New token") {
		t.Fatalf("rotate status = %d", rec.Code)
	}
	store2, _ := access.OpenStore(reg)
	after, _ := store2.Lookup("bob")
	if after.Token == before.Token || after.Token == "" {
		t.Fatal("rotate did not change the token")
	}
}

func TestPanelCreateRejectsEmptyLabel(t *testing.T) {
	s, _ := newTestServer(t)
	rec := doPost(t, s, "/create", url.Values{fieldLabel: {""}, fieldDays: {"30"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("empty-label status = %d, want 303 redirect", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("empty-label should redirect with err=, got %q", rec.Header().Get("Location"))
	}
}

func TestDaysToTTL(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		hours   float64
	}{
		{"", false, 0},
		{"0", false, 0},
		{"30", false, 720},
		{"-1", true, 0},
		{"abc", true, 0},
	}
	for _, c := range cases {
		ttl, err := daysToTTL(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("daysToTTL(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("daysToTTL(%q) error = %v", c.in, err)
		}
		if ttl.Hours() != c.hours {
			t.Fatalf("daysToTTL(%q) = %v hours, want %v", c.in, ttl.Hours(), c.hours)
		}
	}
}
