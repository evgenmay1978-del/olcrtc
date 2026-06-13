package main

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/access"
	"github.com/openlibrecommunity/olcrtc/internal/config"
	"github.com/openlibrecommunity/olcrtc/internal/notify"
)

// errBadDays is returned when the days form field is not a non-negative integer.
var errBadDays = errors.New("days must be a whole number >= 0")

// server holds the panel's configuration. Registry mutations are serialized by
// mu: each request opens the registry file, mutates, and saves, so the file
// stays the single source of truth shared with the server and the CLI.
type server struct {
	registry     string
	user         string
	password     string
	serverConfig string // path to the server YAML, for generating client configs
	payInfo      string // payment instructions shown to clients (phone, etc.)
	notifier     *notify.Telegram
	mu           sync.Mutex
}

func newServer(registry, user, password string) *server {
	return &server{registry: registry, user: user, password: password}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.requireAuth(s.handleIndex))
	mux.HandleFunc("/create", s.requireAuth(s.handleCreate))
	mux.HandleFunc("/disable", s.requireAuth(s.handleDisable))
	mux.HandleFunc("/enable", s.requireAuth(s.handleEnable))
	mux.HandleFunc("/extend", s.requireAuth(s.handleExtend))
	mux.HandleFunc("/rotate", s.requireAuth(s.handleRotate))
	mux.HandleFunc("/delete", s.requireAuth(s.handleDelete))
	mux.HandleFunc("/config", s.requireAuth(s.handleConfig))
	// One-tap approve/reject of a pending payment, used by the Telegram bot
	// (and the web panel). Approve activates with the signed-up tariff's TTL.
	mux.HandleFunc("/approve", s.requireAuth(s.handleApprove))
	mux.HandleFunc("/reject", s.requireAuth(s.handleReject))

	// Public client-facing payment API (no basic auth): the Android app calls
	// these to list tariffs, sign up, and report payment.
	mux.HandleFunc("/api/tariffs", s.handleAPITariffs)
	mux.HandleFunc("/api/signup", s.handleAPISignup)
	mux.HandleFunc("/api/paid", s.handleAPIPaid)
	mux.HandleFunc("/api/status", s.handleAPIStatus)
	// Active clients fetch their ready-to-connect parameters here so the app
	// can seed a working location right after approval.
	mux.HandleFunc("/api/config", s.handleAPIConfig)
	return mux
}

// handleConfig serves a ready-to-run client YAML for the given label, built
// from the server config so the two ends match. The client's access token is
// embedded. Served as a download (client-<label>.yaml).
func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if s.serverConfig == "" {
		http.Error(w, "client config generation is not enabled: start the panel with -server <server.yaml>",
			http.StatusServiceUnavailable)
		return
	}
	label := r.URL.Query().Get("label")
	if label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}

	var token string
	err := s.withStore(false, func(st *access.Store) error {
		c, ok := st.Lookup(label)
		if !ok {
			return fmt.Errorf("%w: %q", access.ErrClientNotFound, label)
		}
		token = c.Token
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	serverCfg, err := config.Load(s.serverConfig)
	if err != nil {
		http.Error(w, "load server config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	yamlBytes, err := config.GenerateClientConfig(serverCfg, token)
	if err != nil {
		http.Error(w, "generate client config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"client-%s.yaml\"", label))
	_, _ = w.Write(yamlBytes)
}

// withStore runs fn against a freshly opened registry under the panel lock,
// then saves if fn reports a change. Reloading per request keeps the panel in
// sync with edits made by the CLI or other tools.
func (s *server) withStore(save bool, fn func(*access.Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := access.OpenStore(s.registry)
	if err != nil {
		return fmt.Errorf("open registry: %w", err)
	}
	if err := fn(store); err != nil {
		return err
	}
	if save {
		if err := store.Save(); err != nil {
			return fmt.Errorf("save registry: %w", err)
		}
	}
	return nil
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	var clients []access.Client
	err := s.withStore(false, func(st *access.Store) error {
		clients = st.Clients()
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msg := r.URL.Query().Get("err"); msg != "" {
		renderIndexErr(w, clients, "Ошибка: "+msg)
		return
	}
	renderIndex(w, clients, "")
}

// handleCreate adds a client. days==0 means no expiry; the new token is shown
// once on the page so the admin can hand it to the client.
func (s *server) handleCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requirePOST(w, r) {
		return
	}
	label := r.FormValue("label")
	contact := r.FormValue("contact")
	if label == "" {
		s.redirectErr(w, r, "label is required")
		return
	}
	ttl, err := daysToTTL(r.FormValue("days"))
	if err != nil {
		s.redirectErr(w, r, err.Error())
		return
	}

	var token string
	var clients []access.Client
	storeErr := s.withStore(true, func(st *access.Store) error {
		tok, addErr := st.Add(label, contact, access.StatusActive, ttl)
		if addErr != nil {
			return fmt.Errorf("add client: %w", addErr)
		}
		token = tok
		return nil
	})
	if storeErr != nil {
		s.redirectErr(w, r, storeErr.Error())
		return
	}
	// Re-read to render the updated list with the new token banner.
	_ = s.withStore(false, func(st *access.Store) error {
		clients = st.Clients()
		return nil
	})
	renderIndex(w, clients, fmt.Sprintf("Created %q. Token (copy now): %s", label, token))
}

func (s *server) handleDisable(w http.ResponseWriter, r *http.Request) {
	s.mutateLabel(w, r, func(st *access.Store, label string) error {
		return st.SetDisabled(label, true)
	})
}

func (s *server) handleEnable(w http.ResponseWriter, r *http.Request) {
	s.mutateLabel(w, r, func(st *access.Store, label string) error {
		return st.SetDisabled(label, false)
	})
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	s.mutateLabel(w, r, func(st *access.Store, label string) error {
		return st.Remove(label)
	})
}

// handleExtend sets a new subscription length in days from now (0 = no expiry)
// and (re)activates the client.
func (s *server) handleExtend(w http.ResponseWriter, r *http.Request) {
	if !s.requirePOST(w, r) {
		return
	}
	label := r.FormValue("label")
	ttl, err := daysToTTL(r.FormValue("days"))
	if err != nil {
		s.redirectErr(w, r, err.Error())
		return
	}
	storeErr := s.withStore(true, func(st *access.Store) error {
		return st.SetStatus(label, access.StatusActive, ttl)
	})
	if storeErr != nil {
		s.redirectErr(w, r, storeErr.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleRotate(w http.ResponseWriter, r *http.Request) {
	if !s.requirePOST(w, r) {
		return
	}
	label := r.FormValue("label")
	var token string
	storeErr := s.withStore(true, func(st *access.Store) error {
		tok, rotErr := st.Rotate(label)
		if rotErr != nil {
			return fmt.Errorf("rotate token: %w", rotErr)
		}
		token = tok
		return nil
	})
	if storeErr != nil {
		s.redirectErr(w, r, storeErr.Error())
		return
	}
	var clients []access.Client
	_ = s.withStore(false, func(st *access.Store) error {
		clients = st.Clients()
		return nil
	})
	renderIndex(w, clients, fmt.Sprintf("Rotated %q. New token (copy now): %s", label, token))
}

// mutateLabel is the shared body for POST handlers that act on one label and
// then redirect back to the index.
func (s *server) mutateLabel(w http.ResponseWriter, r *http.Request, fn func(*access.Store, string) error) {
	if !s.requirePOST(w, r) {
		return
	}
	label := r.FormValue("label")
	if label == "" {
		s.redirectErr(w, r, "label is required")
		return
	}
	if err := s.withStore(true, func(st *access.Store) error { return fn(st, label) }); err != nil {
		s.redirectErr(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func (s *server) redirectErr(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/?err="+msg, http.StatusSeeOther)
	_ = r
}

// daysToTTL converts a days form value to a duration. Empty or "0" means no
// expiry (ttl 0). Negative or non-numeric input is an error.
func daysToTTL(v string) (time.Duration, error) {
	if v == "" {
		return 0, nil
	}
	days, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", errBadDays, v)
	}
	if days < 0 {
		return 0, fmt.Errorf("%w: %d", errBadDays, days)
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// sortedByLabel is a tiny helper kept for clarity at call sites that need a
// stable display order independent of Store internals.
func sortedByLabel(clients []access.Client) []access.Client {
	out := make([]access.Client, len(clients))
	copy(out, clients)
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
