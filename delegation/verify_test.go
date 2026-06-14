package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/harness/errs"
	"github.com/htekdev/ai-harness/hooks"
	"github.com/htekdev/ai-harness/scripting"
)

// fakeCompletionServer returns scripted assistant responses in order.
// One response per HTTP call; subsequent calls beyond the script reuse
// the last response. Tracks total request count for assertions.
func fakeCompletionServer(t *testing.T, responses ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	if len(responses) == 0 {
		t.Fatal("fakeCompletionServer needs at least one scripted response")
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		idx := int(calls.Add(1)) - 1
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: responses[idx]},
				FinishReason: "stop",
			}},
		})
	}))
	return srv, &calls
}

func newTestDelegator(t *testing.T, baseURL string, hookSystem *hooks.System) *Delegator {
	t.Helper()
	return NewDelegator(DelegatorConfig{
		Client: completion.NewClient(completion.ClientConfig{
			BaseURL: baseURL,
			APIKey:  "test-key",
		}),
		Engine:     scripting.NewEngine(),
		HookSystem: hookSystem,
	})
}

// passes-first-try: the verifier accepts the initial run; the delegate
// is invoked exactly once and the original task is the prompt seen by
// the model.
func TestDelegator_Verify_PassesFirstTry(t *testing.T) {
	srv, calls := fakeCompletionServer(t, "I did the thing")
	defer srv.Close()

	d := newTestDelegator(t, srv.URL, nil)

	result, err := d.Execute(context.Background(), Request{
		Task: "create the thing",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "no-op",
			Script:      "def run(args):\n    return \"ok\"",
		}},
		Verify: `
def run(result):
    return json.encode({"verified": True, "reason": ""})
`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Response != "I did the thing" {
		t.Fatalf("response = %q", result.Response)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 completion call, got %d", got)
	}
}

// fails-then-passes: the verifier rejects the first attempt; the
// Delegator re-prompts the SAME delegate with the failure reason,
// the verifier then accepts. The model must be invoked exactly twice
// and the second call must see a "VERIFICATION FAILED" prompt.
func TestDelegator_Verify_FailsThenPasses(t *testing.T) {
	var lastPrompt atomic.Value // string
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req completion.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(req.Messages) > 0 {
			lastPrompt.Store(req.Messages[len(req.Messages)-1].Content)
		}
		idx := calls.Add(1)
		body := "first try (lying)"
		if idx >= 2 {
			body = "actually did it"
		}
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: body},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	d := newTestDelegator(t, srv.URL, nil)

	// Verify accepts only when the response contains the word "actually".
	verifyScript := `
def run(result):
    if "actually" in result["response"]:
        return json.encode({"verified": True, "reason": ""})
    return json.encode({"verified": False, "reason": "delegate did not actually finish"})
`

	result, err := d.Execute(context.Background(), Request{
		Task: "do the work",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "no-op",
			Script:      "def run(args):\n    return \"ok\"",
		}},
		Verify:           verifyScript,
		MaxVerifyRetries: 2,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Response != "actually did it" {
		t.Fatalf("response = %q", result.Response)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected exactly 2 completion calls, got %d", got)
	}
	prompt, _ := lastPrompt.Load().(string)
	if prompt == "" || !containsSub(prompt, "VERIFICATION FAILED") {
		t.Fatalf("retry prompt did not contain failure context, got: %q", prompt)
	}
	if !containsSub(prompt, "delegate did not actually finish") {
		t.Fatalf("retry prompt did not propagate verifier reason, got: %q", prompt)
	}
}

// exhaust-retries: verifier never accepts; Delegator returns
// errs.KindVerificationFailed after MaxVerifyRetries+1 total attempts.
func TestDelegator_Verify_ExhaustsRetries(t *testing.T) {
	srv, calls := fakeCompletionServer(t, "claim", "claim", "claim", "claim", "claim")
	defer srv.Close()

	d := newTestDelegator(t, srv.URL, nil)

	_, err := d.Execute(context.Background(), Request{
		Task: "do the work",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "no-op",
			Script:      "def run(args):\n    return \"ok\"",
		}},
		Verify: `
def run(result):
    return json.encode({"verified": False, "reason": "still not done"})
`,
		MaxVerifyRetries: 2,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ee *errs.Error
	if !errors.As(err, &ee) {
		t.Fatalf("expected typed errs.Error, got %T: %v", err, err)
	}
	if ee.Kind != errs.KindVerificationFailed {
		t.Fatalf("expected KindVerificationFailed, got %v: %v", ee.Kind, err)
	}
	// 1 initial + 2 retries = 3 calls
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected exactly 3 completion calls (1 initial + 2 retries), got %d", got)
	}
}

// post_verify hook event: a registered hook can vote on verification by
// returning ActionBlock. Loop semantics mirror the inline verify script.
func TestDelegator_Verify_HookEventBlocksThenAccepts(t *testing.T) {
	srv, calls := fakeCompletionServer(t, "first", "second")
	defer srv.Close()

	hookSystem := hooks.NewSystem()
	var hookCalls atomic.Int32
	hookSystem.Register(hooks.Registration{
		Name:  "post-verify-blocker",
		Event: hooks.EventDelegatePostVerify,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			n := hookCalls.Add(1)
			if n == 1 {
				return hooks.Result{Action: hooks.ActionBlock, Reason: "first attempt looked wrong"}
			}
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	d := newTestDelegator(t, srv.URL, hookSystem)

	result, err := d.Execute(context.Background(), Request{
		Task: "do the work",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "no-op",
			Script:      "def run(args):\n    return \"ok\"",
		}},
		MaxVerifyRetries: 1,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Response != "second" {
		t.Fatalf("response = %q (expected second)", result.Response)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 completion calls, got %d", got)
	}
	if got := hookCalls.Load(); got != 2 {
		t.Fatalf("expected 2 post_verify hook calls, got %d", got)
	}
}

// no-verify: when neither Verify nor a post_verify hook is set, the
// Ralph loop is bypassed entirely (single attempt, no retry).
func TestDelegator_Verify_DisabledByDefault(t *testing.T) {
	srv, calls := fakeCompletionServer(t, "ok")
	defer srv.Close()

	d := newTestDelegator(t, srv.URL, nil)
	_, err := d.Execute(context.Background(), Request{
		Task: "task",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "no-op",
			Script:      "def run(args):\n    return \"ok\"",
		}},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call (verify disabled), got %d", got)
	}
}

// bad-script: a verify script that fails to compile/run is surfaced as
// errs.KindVerificationFailed (a hard stop, NOT silently treated as
// verified=false).
func TestDelegator_Verify_BadScriptIsHardError(t *testing.T) {
	srv, _ := fakeCompletionServer(t, "ok")
	defer srv.Close()

	d := newTestDelegator(t, srv.URL, nil)
	_, err := d.Execute(context.Background(), Request{
		Task: "task",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "no-op",
			Script:      "def run(args):\n    return \"ok\"",
		}},
		Verify: "this is not valid starlark!!",
	})
	if err == nil {
		t.Fatal("expected error from bad verify script")
	}
	var ee *errs.Error
	if !errors.As(err, &ee) {
		t.Fatalf("expected typed errs.Error, got %T: %v", err, err)
	}
	if ee.Kind != errs.KindVerificationFailed {
		t.Fatalf("expected KindVerificationFailed, got %v: %v", ee.Kind, err)
	}
}

func containsSub(haystack, needle string) bool {
	return indexOfSub(haystack, needle) >= 0
}

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
