package input

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TelegramSource produces Events from Telegram Bot API getUpdates long-polls.
//
// Each incoming message becomes one Event with:
//   - SourceName = "telegram"
//   - SessionKey = chat_id (so each chat owns an isolated session)
//   - Text       = message.text
//   - Metadata   = chat_id, message_id, user_id, username
//
// TelegramSource implements Replier: Reply(ev, text) sends text back to the
// originating chat via sendMessage so a serve loop can route Harness output
// to the right user without each tool re-resolving the chat_id.
//
// Security: ChatAllowlist MUST be non-empty in v1. An empty allowlist returns
// an error from NewTelegramSource — public bots without filtering are a foot-gun.
type TelegramSource struct {
	token         string
	allowlist     map[int64]struct{}
	pollTimeoutS  int
	httpClient    *http.Client
	apiBase       string // e.g. "https://api.telegram.org" — overridable for tests
	offset        int64  // last update_id seen + 1
	updates       []tgUpdate
	updatesCursor int
}

// TelegramConfig configures a TelegramSource.
type TelegramConfig struct {
	// Token is the Bot API token (required).
	Token string

	// ChatAllowlist enumerates the chat IDs that may inject input. MUST be non-empty.
	ChatAllowlist []int64

	// PollTimeoutSeconds is the long-poll timeout in seconds (default 25).
	// Telegram caps this at 50.
	PollTimeoutSeconds int

	// HTTPClient is optional; defaults to a 60s-timeout client.
	HTTPClient *http.Client

	// APIBase overrides "https://api.telegram.org" (for tests/mocks).
	APIBase string
}

// NewTelegramSource constructs a TelegramSource. Returns an error if Token is empty
// or ChatAllowlist is empty (no wildcard allowed in v1).
func NewTelegramSource(cfg TelegramConfig) (*TelegramSource, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("telegram: token is required")
	}
	if len(cfg.ChatAllowlist) == 0 {
		return nil, fmt.Errorf("telegram: chat_allowlist must contain at least one chat_id (no wildcard in v1)")
	}
	allow := make(map[int64]struct{}, len(cfg.ChatAllowlist))
	for _, id := range cfg.ChatAllowlist {
		allow[id] = struct{}{}
	}
	timeout := cfg.PollTimeoutSeconds
	if timeout <= 0 {
		timeout = 25
	}
	if timeout > 50 {
		timeout = 50
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: time.Duration(timeout+10) * time.Second}
	}
	apiBase := cfg.APIBase
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	return &TelegramSource{
		token:        cfg.Token,
		allowlist:    allow,
		pollTimeoutS: timeout,
		httpClient:   client,
		apiBase:      apiBase,
	}, nil
}

// Name returns the source identifier.
func (t *TelegramSource) Name() string { return "telegram" }

// Read returns the next allowlisted message as an Event. It long-polls
// getUpdates and skips messages from chats not in the allowlist.
func (t *TelegramSource) Read(ctx context.Context) (Event, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		// Drain any buffered updates from the previous poll first.
		for t.updatesCursor < len(t.updates) {
			u := t.updates[t.updatesCursor]
			t.updatesCursor++
			if u.UpdateID >= t.offset {
				t.offset = u.UpdateID + 1
			}
			if u.Message == nil || u.Message.Text == "" {
				continue
			}
			chatID := u.Message.Chat.ID
			if _, ok := t.allowlist[chatID]; !ok {
				continue
			}
			return Event{
				SourceName: t.Name(),
				SessionKey: strconv.FormatInt(chatID, 10),
				Text:       u.Message.Text,
				Metadata: map[string]string{
					"chat_id":    strconv.FormatInt(chatID, 10),
					"message_id": strconv.FormatInt(u.Message.MessageID, 10),
					"user_id":    strconv.FormatInt(u.Message.From.ID, 10),
					"username":   u.Message.From.Username,
				},
			}, nil
		}
		// No buffered updates — poll for more.
		if err := t.poll(ctx); err != nil {
			return Event{}, err
		}
	}
}

// Reply sends text back to the chat that produced ev. Implements Replier.
func (t *TelegramSource) Reply(ctx context.Context, ev Event, text string) error {
	chatID := ev.Metadata["chat_id"]
	if chatID == "" {
		chatID = ev.SessionKey
	}
	if chatID == "" {
		return fmt.Errorf("telegram: cannot reply, missing chat_id in event")
	}
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBase, t.token)
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram sendMessage: %s: %s", resp.Status, string(body))
	}
	return nil
}

// Close is a no-op; the HTTP client and offset are GC-managed.
func (t *TelegramSource) Close() error { return nil }

// poll performs one getUpdates call and buffers the results.
func (t *TelegramSource) poll(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/bot%s/getUpdates", t.apiBase, t.token)
	q := url.Values{}
	q.Set("timeout", strconv.Itoa(t.pollTimeoutS))
	q.Set("offset", strconv.FormatInt(t.offset, 10))
	q.Set("allowed_updates", `["message"]`)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram getUpdates: %s: %s", resp.Status, string(body))
	}
	var payload tgUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("telegram getUpdates: decode: %w", err)
	}
	if !payload.OK {
		return fmt.Errorf("telegram getUpdates: not ok")
	}
	t.updates = payload.Result
	t.updatesCursor = 0
	return nil
}

// --- Telegram API types (subset) ---

type tgUpdatesResponse struct {
	OK     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message,omitempty"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	From      tgUser `json:"from"`
	Chat      tgChat `json:"chat"`
	Text      string `json:"text"`
}

type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type tgChat struct {
	ID int64 `json:"id"`
}
