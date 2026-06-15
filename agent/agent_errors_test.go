package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/htekdev/ai-harness/completion"
	agentctx "github.com/htekdev/ai-harness/context"
	"github.com/htekdev/ai-harness/harness/errs"
)

// Phase 5.3: agent runtime errors are typed so retries / dashboards /
// hooks can react to *kind* of failure without parsing message text.

func TestRun_NoChoices_IsKindCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(completion.Response{Choices: nil})
	}))
	defer srv.Close()

	client := completion.NewClient(completion.ClientConfig{BaseURL: srv.URL, APIKey: "k", MaxRetries: 1})
	a := New(Options{
		Client:  client,
		Context: agentctx.NewManager(agentctx.Config{SystemPrompt: "x"}),
	})

	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if k := errs.KindOf(err); k != errs.KindCompletion {
		t.Fatalf("KindOf = %v, want KindCompletion (err=%v)", k, err)
	}
	if errs.IsRetriable(err) {
		t.Fatalf("empty-choices is a logical error, not retriable")
	}
}

func TestRun_ProviderError_IsRetriableCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := completion.NewClient(completion.ClientConfig{BaseURL: srv.URL, APIKey: "k", MaxRetries: 1})
	a := New(Options{
		Client:  client,
		Context: agentctx.NewManager(agentctx.Config{SystemPrompt: "x"}),
	})

	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if k := errs.KindOf(err); k != errs.KindCompletion {
		t.Fatalf("KindOf = %v, want KindCompletion", k)
	}
	if !errs.IsRetriable(err) {
		t.Fatalf("provider failure should be flagged retriable so backoff hooks fire")
	}
}

// Regression for the live Telegram-bot bug: when a Copilot/OpenAI provider
// returns finish_reason="length" (response truncated by max_tokens), the
// loop must NOT silently exit as "successful with text-only reply" — that
// produced reports of the agent saying "let me try X" and then stopping.
func TestRun_FinishReasonLength_IsRetriableTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Index: 0,
				Message: completion.Message{
					Role:    completion.RoleAssistant,
					Content: "hm, that didn't work, let me try",
				},
				FinishReason: "length",
			}},
		})
	}))
	defer srv.Close()

	client := completion.NewClient(completion.ClientConfig{BaseURL: srv.URL, APIKey: "k", MaxRetries: 1})
	a := New(Options{
		Client:  client,
		Context: agentctx.NewManager(agentctx.Config{SystemPrompt: "x"}),
	})

	res, err := a.Run(context.Background(), "do a thing")
	if err == nil {
		t.Fatalf("expected truncation error, got result=%+v", res)
	}
	if k := errs.KindOf(err); k != errs.KindCompletion {
		t.Fatalf("KindOf = %v, want KindCompletion", k)
	}
	if !errs.IsRetriable(err) {
		t.Fatalf("truncation should be retriable so operators can raise max_tokens and retry")
	}
}

// Companion regression: provider claims tool_calls but parser surfaced none.
// Almost always a streaming/format mismatch — must be loud, not silent-done.
func TestRun_FinishReasonToolCalls_NoneParsed_IsRetriable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Index: 0,
				Message: completion.Message{
					Role:    completion.RoleAssistant,
					Content: "",
				},
				FinishReason: "tool_calls",
			}},
		})
	}))
	defer srv.Close()

	client := completion.NewClient(completion.ClientConfig{BaseURL: srv.URL, APIKey: "k", MaxRetries: 1})
	a := New(Options{
		Client:  client,
		Context: agentctx.NewManager(agentctx.Config{SystemPrompt: "x"}),
	})

	_, err := a.Run(context.Background(), "do a thing")
	if err == nil {
		t.Fatal("expected degenerate-tool_calls error")
	}
	if k := errs.KindOf(err); k != errs.KindCompletion {
		t.Fatalf("KindOf = %v, want KindCompletion", k)
	}
	if !errs.IsRetriable(err) {
		t.Fatal("degenerate tool_calls should be retriable")
	}
}
