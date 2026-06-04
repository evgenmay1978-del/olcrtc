package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openlibrecommunity/olcrtc/internal/access"
	"github.com/openlibrecommunity/olcrtc/internal/billing"
)

// maxLabelLen bounds a client-supplied login so the registry stays tidy and
// the value fits UI columns.
const maxLabelLen = 40

// errLoginTaken is returned by signup when the requested login already exists.
var errLoginTaken = errors.New("login already exists")

// statusKey is the JSON field carrying a client's lifecycle state.
const statusKey = "status"

// writeJSON serializes v as a JSON response. Marshal errors are surfaced via a
// 500 so a malformed payload is never silently swallowed.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleAPITariffs returns the tariff catalog for the app to render.
func (s *server) handleAPITariffs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tariffs": billing.Catalog()})
}

// signupRequest is the body the app posts to /api/signup.
type signupRequest struct {
	Login  string `json:"login"`
	Tariff string `json:"tariff"`
}

// handleAPISignup registers a new client as pending for the chosen tariff and
// returns the payment instructions the app should display. The login is the
// label the operator will see in the panel. If the login already exists the
// request is rejected so one client cannot overwrite another.
func (s *server) handleAPISignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	login := strings.TrimSpace(req.Login)
	if login == "" || len(login) > maxLabelLen {
		apiError(w, http.StatusBadRequest, "login required, max 40 chars")
		return
	}
	tariff, err := billing.Lookup(req.Tariff)
	if err != nil {
		apiError(w, http.StatusBadRequest, "unknown tariff")
		return
	}

	// Create the client as pending with the tariff's TTL as the payment
	// deadline, and the chosen tariff recorded as the contact note.
	contact := fmt.Sprintf("tariff %s (%d₽)", tariff.ID, tariff.PriceRUB)
	storeErr := s.withStore(true, func(st *access.Store) error {
		if _, ok := st.Lookup(login); ok {
			return errLoginTaken
		}
		_, addErr := st.Add(login, contact, access.StatusPending, tariff.TTL())
		if addErr != nil {
			return fmt.Errorf("add client: %w", addErr)
		}
		return nil
	})
	if storeErr != nil {
		if errors.Is(storeErr, errLoginTaken) {
			apiError(w, http.StatusConflict, "login already exists, choose another")
			return
		}
		apiError(w, http.StatusInternalServerError, storeErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"login":   login,
		"tariff":  tariff,
		"payInfo": s.payInfo,
		"message": fmt.Sprintf("Переведите %d₽ и нажмите «Я оплатил». В переводе укажите логин: %s",
			tariff.PriceRUB, login),
	})
}

// paidRequest is the body the app posts to /api/paid.
type paidRequest struct {
	Login string `json:"login"`
}

// handleAPIPaid is the client's "I paid" button: it notifies the operator (via
// Telegram, if configured) that the given login claims to have paid, so the
// operator can verify the transfer and approve in the panel.
func (s *server) handleAPIPaid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req paidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	login := strings.TrimSpace(req.Login)
	if login == "" {
		apiError(w, http.StatusBadRequest, "login required")
		return
	}

	// Confirm the client exists and is pending before bothering the operator.
	var contact string
	err := s.withStore(false, func(st *access.Store) error {
		c, ok := st.Lookup(login)
		if !ok {
			return fmt.Errorf("%w: %q", access.ErrClientNotFound, login)
		}
		contact = c.Contact
		return nil
	})
	if err != nil {
		apiError(w, http.StatusNotFound, "login not found, sign up first")
		return
	}

	if s.notifier != nil {
		msg := fmt.Sprintf("💳 Заявка на оплату\nЛогин: %s\n%s\nПодтвердите или отклоните в панели.",
			login, contact)
		if nErr := s.notifier.Notify(r.Context(), msg); nErr != nil {
			// Notification failure must not lose the claim; log via response.
			writeJSON(w, http.StatusOK, map[string]any{
				statusKey: "pending",
				"warning": "operator notification failed; they can still see the request in the panel",
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{statusKey: "pending"})
}

// clientView is the normalized status of one client for the status endpoint.
type clientView struct {
	found  bool
	status string
	token  string
	expiry string
}

// lookupView resolves a login to its normalized status view.
func (s *server) lookupView(login string) clientView {
	var v clientView
	_ = s.withStore(false, func(st *access.Store) error {
		c, ok := st.Lookup(login)
		if !ok {
			return nil
		}
		v.found = true
		v.status = c.Status
		if v.status == "" {
			v.status = access.StatusActive
		}
		if c.Disabled {
			v.status = "disabled"
		}
		v.token = c.Token
		if !c.Expires.IsZero() {
			v.expiry = c.Expires.Format("2006-01-02")
		}
		return nil
	})
	return v
}

// handleAPIStatus lets the app poll a login's current state so it can sync:
// pending / active / rejected / not_found, plus expiry when active.
func (s *server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	login := strings.TrimSpace(r.URL.Query().Get("login"))
	if login == "" {
		apiError(w, http.StatusBadRequest, "login query param required")
		return
	}
	v := s.lookupView(login)
	if !v.found {
		writeJSON(w, http.StatusOK, map[string]any{statusKey: "not_found"})
		return
	}
	resp := map[string]any{statusKey: v.status, "expires": v.expiry}
	// Only hand back the token once access is active, so a pending client
	// cannot use it before the operator approves.
	if v.status == access.StatusActive {
		resp["token"] = v.token
	}
	writeJSON(w, http.StatusOK, resp)
}
