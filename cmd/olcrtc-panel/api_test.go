package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/access"
)

// apiPost posts a JSON body to an unauthenticated API endpoint.
func apiPost(t *testing.T, s *server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func apiGet(t *testing.T, s *server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func TestAPITariffs(t *testing.T) {
	s, _ := newTestServer(t)
	rec := apiGet(t, s, "/api/tariffs")
	if rec.Code != http.StatusOK {
		t.Fatalf("tariffs status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"400", "780", "1100", "2200"} {
		if !strings.Contains(body, want) {
			t.Fatalf("tariffs missing price %s: %s", want, body)
		}
	}
}

func TestAPISignupFlow(t *testing.T) {
	s, reg := newTestServer(t)
	s.payInfo = "pay to +7..."

	// Signup creates a pending client and returns pay info.
	rec := apiPost(t, s, "/api/signup", `{"login":"maria","tariff":"3m"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pay to +7") {
		t.Fatalf("signup missing payInfo: %s", rec.Body.String())
	}

	// The client is pending in the registry, with no access yet.
	store, _ := access.OpenStore(reg)
	c, ok := store.Lookup("maria")
	if !ok || c.Status != access.StatusPending {
		t.Fatalf("maria not pending: %+v ok=%v", c, ok)
	}

	// Status shows pending and withholds the token.
	rec = apiGet(t, s, "/api/status?login=maria")
	var st map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st["status"] != access.StatusPending {
		t.Fatalf("status = %v, want pending", st["status"])
	}
	if _, hasToken := st["token"]; hasToken {
		t.Fatal("pending status must not expose the token")
	}

	// Duplicate login is rejected.
	if rec := apiPost(t, s, "/api/signup", `{"login":"maria","tariff":"1m"}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate signup = %d, want 409", rec.Code)
	}
}

func TestAPIPaidThenApproveActivates(t *testing.T) {
	s, _ := newTestServer(t)
	if rec := apiPost(t, s, "/api/signup", `{"login":"maria","tariff":"3m"}`); rec.Code != http.StatusOK {
		t.Fatalf("signup = %d", rec.Code)
	}

	// "I paid" succeeds (no notifier configured -> still pending).
	if rec := apiPost(t, s, "/api/paid", `{"login":"maria"}`); rec.Code != http.StatusOK {
		t.Fatalf("paid status = %d", rec.Code)
	}

	// Operator approves via the panel; status then reports active + token.
	approve := doPost(t, s, "/extend", url.Values{fieldLabel: {"maria"}, fieldDays: {"90"}})
	if approve.Code != http.StatusSeeOther {
		t.Fatalf("approve = %d", approve.Code)
	}
	rec := apiGet(t, s, "/api/status?login=maria")
	var st map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st["status"] != access.StatusActive {
		t.Fatalf("status = %v, want active", st["status"])
	}
	if tok, _ := st["token"].(string); tok == "" {
		t.Fatal("active status must expose the token")
	}
}

func TestAPISignupRejectsBadInput(t *testing.T) {
	s, _ := newTestServer(t)
	// Unknown tariff.
	if rec := apiPost(t, s, "/api/signup", `{"login":"x","tariff":"99m"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad tariff = %d, want 400", rec.Code)
	}
	// Empty login.
	if rec := apiPost(t, s, "/api/signup", `{"login":"","tariff":"1m"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty login = %d, want 400", rec.Code)
	}
}

func TestAPIPaidUnknownLogin(t *testing.T) {
	s, _ := newTestServer(t)
	if rec := apiPost(t, s, "/api/paid", `{"login":"ghost"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("paid ghost = %d, want 404", rec.Code)
	}
}

func TestAPIStatusNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	rec := apiGet(t, s, "/api/status?login=nobody")
	if !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("status nobody = %s, want not_found", rec.Body.String())
	}
}
