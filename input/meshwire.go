package input

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/htekdev/ai-harness/harness/errs"
)

// MeshWireSource produces Events from a MeshWire mesh
// (https://meshwire.io). It long-polls
// `GET /mesh/:meshId/messages?recipient=<agent_id>` and turns each delivered
// mesh message into a single Event:
//
//   - SourceName = "meshwire"
//   - SessionKey = sender_id (so each peer agent owns an isolated session)
//   - Text       = content
//   - Metadata   = mesh_id, message_id, message_uid, sender_id, recipient_id, priority
//
// MeshWireSource implements Replier: Reply(ev, text) posts to
// `POST /mesh/:meshId/messages/<message_id>/reply` with the configured AgentID
// as sender_id, so harness output flows back to the originating peer agent
// without each tool re-resolving the mesh routing.
//
// Security: SenderAllowlist MUST be non-empty in v1. An empty allowlist
// returns an error from NewMeshWireSource — open meshes without filtering
// are a foot-gun for any harness driving real tool execution.
type MeshWireSource struct {
	token        string
	meshID       string
	agentID      string
	allowlist    map[string]struct{}
	pollTimeoutS int
	httpClient   *http.Client
	apiBase      string
	lastSeenID   int64
	offsetStore  OffsetStore
	buffered     []mwMessage
	cursor       int
}

// MeshWireConfig configures a MeshWireSource.
type MeshWireConfig struct {
	// Token is the MeshWire API token (required, e.g. "mw_<64hex>").
	Token string

	// MeshID is the mesh to attach to (required).
	MeshID string

	// AgentID is this harness's identity in the mesh (required). Used as the
	// `recipient` filter on long-polls and as `sender_id` on replies.
	AgentID string

	// SenderAllowlist enumerates peer agent IDs whose messages may inject
	// input into this harness session. MUST be non-empty in v1.
	SenderAllowlist []string

	// PollTimeoutSeconds is the long-poll timeout in seconds (default 30).
	// Server caps this at 60.
	PollTimeoutSeconds int

	// HTTPClient is optional; defaults to a (timeout+10)s client.
	HTTPClient *http.Client

	// APIBase overrides "https://meshwire.io" (for tests/mocks/self-host).
	APIBase string

	// OffsetStore persists the last-seen message_id across process restarts.
	// If nil, the offset lives only in memory and previously-delivered
	// messages may be re-processed after a crash.
	OffsetStore OffsetStore
}

// NewMeshWireSource constructs a MeshWireSource. Returns an error if Token,
// MeshID, or AgentID is empty, or SenderAllowlist is empty.
func NewMeshWireSource(cfg MeshWireConfig) (*MeshWireSource, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errs.Newf(errs.KindSource, "input.meshwire.new", "meshwire: token is required")
	}
	if strings.TrimSpace(cfg.MeshID) == "" {
		return nil, errs.Newf(errs.KindSource, "input.meshwire.new", "meshwire: mesh_id is required")
	}
	if strings.TrimSpace(cfg.AgentID) == "" {
		return nil, errs.Newf(errs.KindSource, "input.meshwire.new", "meshwire: agent_id is required")
	}
	if len(cfg.SenderAllowlist) == 0 {
		return nil, errs.Newf(errs.KindSource, "input.meshwire.new", "meshwire: sender_allowlist must contain at least one agent_id (no wildcard in v1)")
	}
	allow := make(map[string]struct{}, len(cfg.SenderAllowlist))
	for _, id := range cfg.SenderAllowlist {
		allow[id] = struct{}{}
	}
	timeout := cfg.PollTimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 60 {
		timeout = 60
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: time.Duration(timeout+10) * time.Second}
	}
	apiBase := cfg.APIBase
	if apiBase == "" {
		apiBase = "https://meshwire.io"
	}
	src := &MeshWireSource{
		token:        cfg.Token,
		meshID:       cfg.MeshID,
		agentID:      cfg.AgentID,
		allowlist:    allow,
		pollTimeoutS: timeout,
		httpClient:   client,
		apiBase:      apiBase,
		offsetStore:  cfg.OffsetStore,
	}
	if src.offsetStore != nil {
		stored, err := src.offsetStore.Load()
		if err != nil {
			return nil, errs.Wrap(errs.KindPersistence, "input.meshwire.new", err, "meshwire: load offset")
		}
		if stored < 0 {
			stored = 0
		}
		src.lastSeenID = stored
	}
	return src, nil
}

// Name returns the source identifier.
func (m *MeshWireSource) Name() string { return "meshwire" }

// Read returns the next allowlisted mesh message as an Event.
//
// Filtering: messages whose sender_id is not in the allowlist are silently
// dropped; messages whose message_id <= lastSeenID are also dropped (so
// the client tolerates a server that ignores the `offset` query parameter
// — which is the current MeshWire behavior). The lastSeenID advances for
// every message inspected, including filtered ones, so the offset always
// reflects the latest delivery the client has acknowledged.
func (m *MeshWireSource) Read(ctx context.Context) (Event, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		// Drain any buffered messages from the previous poll.
		for m.cursor < len(m.buffered) {
			msg := m.buffered[m.cursor]
			m.cursor++
			if msg.MessageID <= m.lastSeenID {
				continue
			}
			m.lastSeenID = msg.MessageID
			if m.offsetStore != nil {
				if err := m.offsetStore.Save(m.lastSeenID); err != nil {
					return Event{}, errs.Wrap(errs.KindPersistence, "input.meshwire.read", err, "meshwire: persist offset")
				}
			}
			if msg.Content == "" {
				continue
			}
			if _, ok := m.allowlist[msg.SenderID]; !ok {
				continue
			}
			meta := map[string]string{
				"mesh_id":      m.meshID,
				"message_id":   strconv.FormatInt(msg.MessageID, 10),
				"message_uid":  msg.MessageUID,
				"sender_id":    msg.SenderID,
				"recipient_id": msg.RecipientID,
				"priority":     msg.Priority,
			}
			return Event{
				SourceName: m.Name(),
				SessionKey: msg.SenderID,
				Text:       msg.Content,
				Metadata:   meta,
			}, nil
		}
		// No buffered messages — long-poll for more.
		if err := m.poll(ctx); err != nil {
			return Event{}, err
		}
	}
}

// Reply posts ev's harness response back as a reply on the originating
// mesh message. Implements Replier.
func (m *MeshWireSource) Reply(ctx context.Context, ev Event, text string) error {
	msgIDStr := ev.Metadata["message_id"]
	if msgIDStr == "" {
		return errs.Newf(errs.KindSource, "input.meshwire.reply", "meshwire: cannot reply, missing message_id in event metadata")
	}
	endpoint := fmt.Sprintf("%s/mesh/%s/messages/%s/reply",
		strings.TrimRight(m.apiBase, "/"), m.meshID, msgIDStr)

	body, err := json.Marshal(map[string]string{
		"sender_id": m.agentID,
		"content":   text,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.token)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(resp.Body)
		return errs.Newf(errs.KindSource, "input.meshwire.reply", "meshwire reply: %s: %s", resp.Status, string(raw))
	}
	return nil
}

// Close is a no-op; HTTP client and offset are GC-managed.
func (m *MeshWireSource) Close() error { return nil }

// poll performs one long-poll GET /messages and buffers the result.
func (m *MeshWireSource) poll(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/mesh/%s/messages",
		strings.TrimRight(m.apiBase, "/"), m.meshID)
	q := url.Values{}
	q.Set("recipient", m.agentID)
	q.Set("timeout", strconv.Itoa(m.pollTimeoutS))
	// Server currently ignores `offset` for filtering, but we set it for
	// forward-compat and clarity in request logs. Client-side filtering
	// against lastSeenID is the source of truth.
	q.Set("offset", strconv.FormatInt(m.lastSeenID+1, 10))
	q.Set("limit", "50")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(resp.Body)
		return errs.Newf(errs.KindSource, "input.meshwire.poll", "meshwire getMessages: %s: %s", resp.Status, string(raw))
	}
	var payload mwMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return errs.Wrap(errs.KindSource, "input.meshwire.poll", err, "meshwire getMessages: decode")
	}
	if !payload.OK {
		return errs.Newf(errs.KindSource, "input.meshwire.poll", "meshwire getMessages: not ok")
	}
	m.buffered = payload.Messages
	m.cursor = 0
	return nil
}

// --- MeshWire API types (subset) ---

type mwMessagesResponse struct {
	OK       bool        `json:"ok"`
	Messages []mwMessage `json:"messages"`
	Count    int         `json:"count"`
}

type mwMessage struct {
	MeshID      string `json:"mesh_id"`
	MessageID   int64  `json:"message_id"`
	MessageUID  string `json:"message_uid"`
	SenderID    string `json:"sender_id"`
	RecipientID string `json:"recipient_id"`
	Content     string `json:"content"`
	Priority    string `json:"priority"`
	CreatedAt   string `json:"created_at"`
}
