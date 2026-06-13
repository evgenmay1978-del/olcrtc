// Package notify delivers operator notifications (e.g. a new payment claim) to
// Telegram. It is intentionally tiny: a single sendMessage call against the Bot
// API. A zero/!Enabled notifier is a safe no-op, so callers need no nil checks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// requestTimeout bounds a single Telegram API call so a slow network never
// blocks the request that triggered the notification.
const requestTimeout = 10 * time.Second

// errAPIStatus is returned when the Telegram API responds with a non-200 status.
var errAPIStatus = errors.New("telegram API returned non-OK status")

// Telegram sends messages to a chat via the Bot API. Construct it with
// NewTelegram; the zero value is a disabled no-op notifier.
type Telegram struct {
	token  string
	chatID string
	client *http.Client
	apiURL string // overridable for tests; defaults to the public Bot API
}

// NewTelegram returns a notifier for the given bot token and chat ID. If either
// is empty the notifier is disabled and Notify is a no-op, so the server can
// run without Telegram configured.
func NewTelegram(token, chatID string) *Telegram {
	return &Telegram{
		token:  token,
		chatID: chatID,
		client: &http.Client{Timeout: requestTimeout},
		apiURL: "https://api.telegram.org",
	}
}

// Enabled reports whether the notifier has credentials to send.
func (t *Telegram) Enabled() bool {
	return t != nil && t.token != "" && t.chatID != ""
}

// Button is one inline-keyboard button: a label and the callback_data Telegram
// sends back to the bot when the operator taps it.
type Button struct {
	Text string
	Data string
}

// Notify sends text to the configured chat. It is a no-op when disabled.
func (t *Telegram) Notify(ctx context.Context, text string) error {
	if !t.Enabled() {
		return nil
	}
	return t.send(ctx, map[string]any{"chat_id": t.chatID, "text": text})
}

// NotifyButtons sends text with an inline keyboard (rows of buttons), so the
// operator can act (e.g. approve/reject) straight from the chat. No-op when
// disabled.
func (t *Telegram) NotifyButtons(ctx context.Context, text string, rows [][]Button) error {
	if !t.Enabled() {
		return nil
	}
	// callback_data is Telegram's API field name; tagliatelle's camelCase rule
	// does not apply to a third-party wire format.
	//nolint:tagliatelle
	type tgBtn struct {
		Text         string `json:"text"`
		CallbackData string `json:"callback_data"`
	}
	kb := make([][]tgBtn, 0, len(rows))
	for _, row := range rows {
		out := make([]tgBtn, 0, len(row))
		for _, b := range row {
			out = append(out, tgBtn{Text: b.Text, CallbackData: b.Data})
		}
		kb = append(kb, out)
	}
	return t.send(ctx, map[string]any{
		"chat_id":      t.chatID,
		"text":         text,
		"reply_markup": map[string]any{"inline_keyboard": kb},
	})
}

// send marshals body and POSTs it to the Bot API sendMessage method.
func (t *Telegram) send(ctx context.Context, body map[string]any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.apiURL, t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d", errAPIStatus, resp.StatusCode)
	}
	return nil
}
