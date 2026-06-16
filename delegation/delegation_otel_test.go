package delegation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/hooks"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Phase 5.2 PR-B: span instrumentation for delegation.Execute.

func newDelegationSpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp)),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})
	return exp
}

func attrInt64(attrs []attribute.KeyValue, key string) int64 {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsInt64()
		}
	}
	return -1
}

func attrStr(attrs []attribute.KeyValue, key string) string {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

func TestExecute_EmitsDelegationSpan_RecordsErrorOnDepthLimit(t *testing.T) {
	exp := newDelegationSpanRecorder(t)
	d := NewDelegator(DelegatorConfig{MaxDepth: 2})

	ctx := WithDepth(context.Background(), 2)
	_, err := d.Execute(ctx, Request{
		Task:  "test-task-12-bytes",
		Agent: "myagent",
		Tools: []ToolSpec{{Name: "t", Description: "d", Script: "def run(a):\n    return 'ok'"}},
	})
	if err == nil {
		t.Fatal("expected depth limit error")
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Name != "delegation.execute" {
		t.Errorf("name: got %q want delegation.execute", s.Name)
	}
	if got := attrStr(s.Attributes, "delegation.agent"); got != "myagent" {
		t.Errorf("delegation.agent: got %q want myagent", got)
	}
	if got := attrInt64(s.Attributes, "delegation.depth"); got != 2 {
		t.Errorf("delegation.depth: got %d want 2", got)
	}
	if got := attrInt64(s.Attributes, "delegation.task_len"); got != int64(len("test-task-12-bytes")) {
		t.Errorf("delegation.task_len: got %d want %d", got, len("test-task-12-bytes"))
	}
	if got := attrInt64(s.Attributes, "delegation.tools_count"); got != 1 {
		t.Errorf("delegation.tools_count: got %d want 1", got)
	}
	if s.Status.Code != otelcodes.Error {
		t.Errorf("status: got %v want Error", s.Status.Code)
	}
	foundExc := false
	for _, ev := range s.Events {
		if ev.Name == "exception" {
			foundExc = true
		}
	}
	if !foundExc {
		t.Errorf("expected exception event for depth limit error")
	}
}

func TestExecute_UsesSpanIDsForDelegationCorrelation(t *testing.T) {
	exp := newDelegationSpanRecorder(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{
				Message:      completion.Message{Role: completion.RoleAssistant, Content: "done"},
				FinishReason: "stop",
			}},
		})
	}))
	defer server.Close()

	var requestID, requestParentID, resultID string
	hookSystem := hooks.NewSystem()
	hookSystem.Register(hooks.Registration{
		Name:  "capture-pre",
		Event: hooks.EventDelegatePre,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			req := payload.(*Request)
			requestID = req.ID
			requestParentID = req.ParentID
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})
	hookSystem.Register(hooks.Registration{
		Name:  "capture-post",
		Event: hooks.EventDelegatePost,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			resultID = payload.(*Result).ID
			return hooks.Result{Action: hooks.ActionContinue}
		},
	})

	d := NewDelegator(DelegatorConfig{
		Client: completion.NewClient(completion.ClientConfig{
			BaseURL: server.URL,
			APIKey:  "test-key",
		}),
		HookSystem: hookSystem,
	})

	ctx, parent := otel.Tracer(tracerName).Start(context.Background(), "parent")
	_, err := d.Execute(ctx, Request{
		Task: "trace me",
		Tools: []ToolSpec{{
			Name:        "noop",
			Description: "No-op tool",
			Script:      "def run(args):\n    return \"ok\"",
		}},
	})
	parentID := parent.SpanContext().SpanID().String()
	parent.End()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	spans := exp.GetSpans()
	var delegationSpanID string
	for _, span := range spans {
		if span.Name == "delegation.execute" {
			delegationSpanID = span.SpanContext.SpanID().String()
			break
		}
	}
	if delegationSpanID == "" {
		t.Fatal("expected delegation.execute span")
	}
	if requestID != delegationSpanID || resultID != delegationSpanID {
		t.Fatalf("expected request/result ids %q, got request=%q result=%q", delegationSpanID, requestID, resultID)
	}
	if requestParentID != parentID {
		t.Fatalf("expected parent id %q, got %q", parentID, requestParentID)
	}
	if requestID == (trace.SpanID{}).String() {
		t.Fatal("expected non-zero span id")
	}
}
