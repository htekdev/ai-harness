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
