package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
// loop must AUTO-CONTINUE by injecting "Please continue from where you
// left off." rather than silently exiting with the truncated text or
// erroring out. Mirrors copilot-agent-runtime's MAX_CONTINUATION_ATTEMPTS=3
// pattern. This is exactly what produced reports of the agent saying "let
// me try X" and then stopping.
func TestRun_FinishReasonLength_AutoContinuesAndRecovers(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// First call: model gets truncated mid-thought.
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
			return
		}
		// Second call (after continuation prompt): model finishes cleanly.
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Index: 0,
				Message: completion.Message{
					Role:    completion.RoleAssistant,
					Content: "the other approach instead. Done.",
				},
				FinishReason: "stop",
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
	if err != nil {
		t.Fatalf("expected auto-recovery, got error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result on auto-recovery")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 model calls (truncated + continuation), got %d", got)
	}
	if !strings.Contains(res.Response, "Done") {
		t.Fatalf("expected continuation reply, got %q", res.Response)
	}
}

// Companion: when truncation persists past the continuation budget (3),
// surface a typed retriable error instead of looping forever.
func TestRun_FinishReasonLength_BudgetExhausted_IsRetriable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always truncate — pathological model / pathologically small max_tokens.
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Index: 0,
				Message: completion.Message{
					Role:    completion.RoleAssistant,
					Content: "still cut off",
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

	_, err := a.Run(context.Background(), "do a thing")
	if err == nil {
		t.Fatal("expected exhaustion error after repeated truncations")
	}
	if k := errs.KindOf(err); k != errs.KindCompletion {
		t.Fatalf("KindOf = %v, want KindCompletion", k)
	}
	if !errs.IsRetriable(err) {
		t.Fatal("exhausted-truncation error should be retriable")
	}
}

// Smarter-model planning-turn nudge. When a reasoning model emits text
// content with finish_reason="stop" and zero tool_calls (planning out
// loud instead of acting), the loop must inject a system reminder and
// re-prompt — bounded by Options.MaxEmptyToolCallContinuations. This is
// the bug Hector reported after upgrading the test-harness model.
func TestRun_TextOnlyStop_NudgesAndRecovers(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// Smarter model emits planning text, no tool calls, stop.
			_ = json.NewEncoder(w).Encode(completion.Response{
				Choices: []completion.Choice{{
					Index: 0,
					Message: completion.Message{
						Role:    completion.RoleAssistant,
						Content: "Let me start by reading the README, then check the package.json.",
					},
					FinishReason: "stop",
				}},
			})
			return
		}
		// After the nudge, model finalizes.
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Index: 0,
				Message: completion.Message{
					Role:    completion.RoleAssistant,
					Content: "All done.",
				},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := completion.NewClient(completion.ClientConfig{BaseURL: srv.URL, APIKey: "k", MaxRetries: 1})
	a := New(Options{
		Client:                        client,
		Context:                       agentctx.NewManager(agentctx.Config{SystemPrompt: "x"}),
		MaxEmptyToolCallContinuations: 1,
	})

	res, err := a.Run(context.Background(), "do a thing")
	if err != nil {
		t.Fatalf("expected nudge recovery, got error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 model calls (planning + finalize after 1 nudge), got %d", got)
	}
	if !strings.Contains(res.Response, "All done") {
		t.Fatalf("expected finalized reply, got %q", res.Response)
	}
}

// When MaxEmptyToolCallContinuations is 0 (default), the loop must
// preserve strict OpenAI-spec behavior: text-only + stop = final reply.
func TestRun_TextOnlyStop_DefaultExitsImmediately(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Index: 0,
				Message: completion.Message{
					Role:    completion.RoleAssistant,
					Content: "Here is my answer.",
				},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	client := completion.NewClient(completion.ClientConfig{BaseURL: srv.URL, APIKey: "k", MaxRetries: 1})
	a := New(Options{
		Client:  client,
		Context: agentctx.NewManager(agentctx.Config{SystemPrompt: "x"}),
		// MaxEmptyToolCallContinuations defaults to 0
	})

	res, err := a.Run(context.Background(), "do a thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 model call (no nudging), got %d", got)
	}
	if res.Response != "Here is my answer." {
		t.Fatalf("unexpected response: %q", res.Response)
	}
}
