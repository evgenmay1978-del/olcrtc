package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/openlibrecommunity/olcrtc/internal/access"
	"github.com/openlibrecommunity/olcrtc/internal/billing"
	"github.com/openlibrecommunity/olcrtc/internal/config"
	"github.com/openlibrecommunity/olcrtc/internal/notify"
)

// maxLabelLen bounds a client-supplied login so the registry stays tidy and
// the value fits UI columns.
const maxLabelLen = 40

// errLoginTaken is returned by signup when the requested login already exists.
var errLoginTaken = errors.New("login already exists")

// statusKey is the JSON field carrying a client's lifecycle state.
const statusKey = "status"

// loginKey is the JSON field / query param carrying a client's login.
const loginKey = "login"

// hwidHeader carries the app's stable device id, used to bind a device to a
// client under the strict per-client device cap.
const hwidHeader = "x-hwid"

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
		loginKey:  login,
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

	if !s.notifyClaim(r.Context(), "💳 Заявка на оплату", login, contact) {
		// Notification failure must not lose the claim; surface a warning.
		writeJSON(w, http.StatusOK, map[string]any{
			statusKey: "pending",
			"warning": "operator notification failed; they can still see the request in the panel",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{statusKey: "pending"})
}

// notifyClaim sends the operator a Telegram approve/reject card for a payment or
// renewal claim. Returns true when there is no notifier or the send succeeded;
// false only when a configured notifier failed (the claim still stands in the
// panel either way).
func (s *server) notifyClaim(ctx context.Context, title, login, contact string) bool {
	if s.notifier == nil {
		return true
	}
	msg := fmt.Sprintf("%s\nЛогин: %s\n%s\nПодтвердите или отклоните кнопкой ниже.",
		title, login, contact)
	buttons := [][]notify.Button{{
		{Text: "✅ Подтвердить", Data: "maestrovpn:ap:" + login},
		{Text: "❌ Отклонить", Data: "maestrovpn:rj:" + login},
	}}
	return s.notifier.NotifyButtons(ctx, msg, buttons) == nil
}

// renewRequest is the body the app posts to /api/renew.
type renewRequest struct {
	Login  string `json:"login"`
	Tariff string `json:"tariff"`
}

// handleAPIRenew lets an existing client re-purchase without creating a new
// account: it flags the SAME login pending for the chosen tariff (keeping its
// token, earned expiry, and bound devices) and notifies the operator. On
// approval the new term is stacked onto any remaining time. This is what keeps
// a repeat purchase from leaving the old client behind as garbage.
func (s *server) handleAPIRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req renewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	login := strings.TrimSpace(req.Login)
	if login == "" {
		apiError(w, http.StatusBadRequest, "login required")
		return
	}
	tariff, err := billing.Lookup(req.Tariff)
	if err != nil {
		apiError(w, http.StatusBadRequest, "unknown tariff")
		return
	}
	contact := fmt.Sprintf("%s%s (%d₽)", access.RenewContactPrefix, tariff.ID, tariff.PriceRUB)
	storeErr := s.withStore(true, func(st *access.Store) error {
		if _, ok := st.Lookup(login); !ok {
			return fmt.Errorf("%w: %q", access.ErrClientNotFound, login)
		}
		return st.MarkPending(login, contact)
	})
	if storeErr != nil {
		apiError(w, http.StatusNotFound, "login not found, sign up first")
		return
	}
	if !s.notifyClaim(r.Context(), "♻️ Заявка на продление", login, contact) {
		writeJSON(w, http.StatusOK, map[string]any{
			statusKey: "pending",
			"warning": "operator notification failed; they can still see the request in the panel",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{statusKey: "pending"})
}

// resetRequest is the body the app posts to /api/reset-devices.
type resetRequest struct {
	Login string `json:"login"`
}

// handleAPIResetDevices clears a client's bound devices so the user can move to
// new ones (e.g. after changing a phone) without hitting the cap. The login is
// the user's own secret, so clearing only affects their own access.
func (s *server) handleAPIResetDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	var req resetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	login := strings.TrimSpace(req.Login)
	if login == "" {
		apiError(w, http.StatusBadRequest, "login required")
		return
	}
	storeErr := s.withStore(true, func(st *access.Store) error {
		if _, ok := st.Lookup(login); !ok {
			return fmt.Errorf("%w: %q", access.ErrClientNotFound, login)
		}
		return st.ResetDevices(login)
	})
	if storeErr != nil {
		apiError(w, http.StatusNotFound, "login not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{statusKey: "ok", loginKey: login})
}

// tariffIDFromContact extracts the tariff id from the contact note that signup
// or renew stores, e.g. "tariff 2m (750₽)" or "renew 2m (750₽)" -> "2m".
func tariffIDFromContact(contact string) string {
	for _, p := range []string{"tariff ", access.RenewContactPrefix} {
		i := strings.Index(contact, p)
		if i < 0 {
			continue
		}
		rest := contact[i+len(p):]
		if j := strings.IndexByte(rest, ' '); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return ""
}

// isRenewal reports whether a contact note marks a renewal (vs a first signup).
func isRenewal(contact string) bool {
	return strings.HasPrefix(strings.TrimSpace(contact), access.RenewContactPrefix)
}

// handleApprove activates a pending client for its signed-up tariff's full
// duration. Used by the Telegram approve button and the web panel.
func (s *server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		apiError(w, http.StatusBadRequest, "label required")
		return
	}
	err := s.withStore(true, func(st *access.Store) error {
		c, ok := st.Lookup(label)
		if !ok {
			return fmt.Errorf("%w: %q", access.ErrClientNotFound, label)
		}
		tariff, lErr := billing.Lookup(tariffIDFromContact(c.Contact))
		if lErr != nil {
			return fmt.Errorf("lookup tariff: %w", lErr)
		}
		// A renewal stacks the new term onto the earned remaining time; a first
		// signup just starts the term now. Renew handles both via max(now,expiry).
		if isRenewal(c.Contact) {
			return st.Renew(label, tariff.TTL())
		}
		return st.SetStatus(label, access.StatusActive, tariff.TTL())
	})
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{statusKey: access.StatusActive, loginKey: label})
}

// handleReject declines a pending payment: the client gets no access.
func (s *server) handleReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		apiError(w, http.StatusBadRequest, "label required")
		return
	}
	if err := s.withStore(true, func(st *access.Store) error {
		return st.SetStatus(label, access.StatusRejected, 0)
	}); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{statusKey: access.StatusRejected, loginKey: label})
}

// clientView is the normalized status of one client for the status endpoint.
type clientView struct {
	found   bool
	status  string
	token   string
	expiry  string
	devices int
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
		v.devices = len(c.Devices)
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
	login := strings.TrimSpace(r.URL.Query().Get(loginKey))
	if login == "" {
		apiError(w, http.StatusBadRequest, "login query param required")
		return
	}
	v := s.lookupView(login)
	if !v.found {
		writeJSON(w, http.StatusOK, map[string]any{statusKey: "not_found"})
		return
	}
	resp := map[string]any{
		statusKey:      v.status,
		"expires":      v.expiry,
		"devices":      v.devices,
		"device_limit": access.MaxDevices,
	}
	// Only hand back the token once access is active, so a pending client
	// cannot use it before the operator approves.
	if v.status == access.StatusActive {
		resp["token"] = v.token
	}
	writeJSON(w, http.StatusOK, resp)
}

// connectionConfig is the ready-to-connect bundle the app needs to seed a
// working location: the server-wide room/key/provider/transport plus the
// client's own access token. It mirrors the fields config.GenerateClientConfig
// copies from the server config, so the app end always matches the server end.
// snake_case tags are required: they mirror the app's @SerialName fields and the
// olcrtc client config keys, so tagliatelle's camelCase rule does not apply here.
//
//nolint:tagliatelle
type connectionConfig struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	RoomID     string `json:"room_id"`
	Channel    string `json:"channel,omitempty"`
	Key        string `json:"key"`
	Transport  string `json:"transport"`
	EngineName string `json:"engine_name,omitempty"`
	EngineURL  string `json:"engine_url,omitempty"`
	Token      string `json:"token"`
	Expires    string `json:"expires,omitempty"`
}

// handleAPIConfig hands an ACTIVE client the connection parameters it needs to
// connect, so the app can seed a ready-to-use location right after the operator
// approves a payment. Pending/rejected/disabled clients get 403 with no config,
// so the connection details are never exposed before access is granted.
func (s *server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	if s.serverConfig == "" {
		apiError(w, http.StatusServiceUnavailable,
			"config generation is not enabled on this panel")
		return
	}
	login := strings.TrimSpace(r.URL.Query().Get(loginKey))
	if login == "" {
		apiError(w, http.StatusBadRequest, "login query param required")
		return
	}
	v := s.lookupView(login)
	if !v.found {
		apiError(w, http.StatusNotFound, "login not found")
		return
	}
	if v.status != access.StatusActive {
		apiError(w, http.StatusForbidden, "access is not active")
		return
	}

	// Bind this device under the strict cap. A new device beyond the cap is
	// refused (device_limit) so the app can prompt the user to reset devices.
	// Without an hwid (older app) we skip binding and stay token-only.
	hwid := strings.TrimSpace(r.Header.Get(hwidHeader))
	if hwid != "" {
		bindErr := s.withStore(true, func(st *access.Store) error {
			return st.AddDevice(login, hwid)
		})
		if errors.Is(bindErr, access.ErrDeviceLimit) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error":        "device_limit",
				"device_limit": access.MaxDevices,
			})
			return
		}
		if bindErr != nil {
			apiError(w, http.StatusInternalServerError, "bind device")
			return
		}
	}

	serverCfg, err := config.Load(s.serverConfig)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "load server config")
		return
	}
	writeJSON(w, http.StatusOK, connectionConfig{
		Name:       "MaestroVPN",
		Provider:   serverCfg.Auth.Provider,
		RoomID:     serverCfg.Room.ID,
		Channel:    serverCfg.Room.Channel,
		Key:        serverCfg.Crypto.Key,
		Transport:  serverCfg.Net.Transport,
		EngineName: serverCfg.Engine.Name,
		EngineURL:  serverCfg.Engine.URL,
		Token:      v.token,
		Expires:    v.expiry,
	})
}
