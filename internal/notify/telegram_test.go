package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledNotifierIsNoop(t *testing.T) {
	// Missing credentials -> disabled -> Notify is a no-op, no network call.
	tg := NewTelegram("", "")
	if tg.Enabled() {
		t.Fatal("notifier with empty creds should be disabled")
	}
	if err := tg.Notify(context.Background(), "hello"); err != nil {
		t.Fatalf("disabled Notify should be no-op, got %v", err)
	}
}

func TestNotifySendsToChat(t *testing.T) {
	var gotBody string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tg := NewTelegram("TOKEN", "12345")
	tg.apiURL = srv.URL // point at the test server

	if !tg.Enabled() {
		t.Fatal("notifier with creds should be enabled")
	}
	if err := tg.Notify(context.Background(), "new payment claim"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if !strings.Contains(gotPath, "/botTOKEN/sendMessage") {
		t.Fatalf("path = %q, want bot sendMessage", gotPath)
	}
	if !strings.Contains(gotBody, "12345") || !strings.Contains(gotBody, "new payment claim") {
		t.Fatalf("body missing chat/text: %q", gotBody)
	}
}

func TestNotifyReportsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	tg := NewTelegram("TOKEN", "12345")
	tg.apiURL = srv.URL
	if err := tg.Notify(context.Background(), "x"); err == nil {
		t.Fatal("expected error on non-200 API response")
	}
}
