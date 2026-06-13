package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/htekdev/ai-harness/completion"
	"github.com/htekdev/ai-harness/hooks"
)

// Phase 5.2 PR-B: span instrumentation for agent.Run.

func hookBlockTurnStart() hooks.Registration {
	return hooks.Registration{
		Name:  "blocker",
		Event: hooks.EventTurnStart,
		Handler: func(ctx context.Context, event hooks.Event, payload any) hooks.Result {
			return hooks.Result{Action: hooks.ActionBlock, Reason: "test"}
		},
	}
}

func newAgentSpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
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

func attrLookupString(attrs []attribute.KeyValue, key string) string {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

func attrLookupInt(attrs []attribute.KeyValue, key string) int64 {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsInt64()
		}
	}
	return -1
}

func TestRun_EmitsAgentTurnSpan_Success(t *testing.T) {
	exp := newAgentSpanRecorder(t)
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(completion.Response{
			Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "ok"}, FinishReason: "stop"}},
			Usage:   completion.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		})
	})

	if _, err := agent.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var turn *tracetest.SpanStub
	for i := range exp.GetSpans() {
		s := &exp.GetSpans()[i]
		if s.Name == "agent.turn" {
			turn = s
			break
		}
	}
	if turn == nil {
		t.Fatalf("agent.turn span not emitted; got %d spans", len(exp.GetSpans()))
	}
	if got := attrLookupInt(turn.Attributes, "turn.index"); got != 1 {
		t.Errorf("turn.index: got %d want 1", got)
	}
	if got := attrLookupInt(turn.Attributes, "turn.iterations"); got != 1 {
		t.Errorf("turn.iterations: got %d want 1", got)
	}
	if got := attrLookupInt(turn.Attributes, "turn.tool_calls"); got != 0 {
		t.Errorf("turn.tool_calls: got %d want 0", got)
	}
	if got := attrLookupInt(turn.Attributes, "turn.prompt_tokens"); got != 11 {
		t.Errorf("turn.prompt_tokens: got %d want 11", got)
	}
	if got := attrLookupInt(turn.Attributes, "turn.completion_tokens"); got != 7 {
		t.Errorf("turn.completion_tokens: got %d want 7", got)
	}
	if got := attrLookupInt(turn.Attributes, "turn.total_tokens"); got != 18 {
		t.Errorf("turn.total_tokens: got %d want 18", got)
	}
	if turn.Status.Code == otelcodes.Error {
		t.Errorf("status: unexpected Error on success")
	}
}

func TestRun_EmitsAgentTurnSpan_WithToolCall_NestsToolSpan(t *testing.T) {
	exp := newAgentSpanRecorder(t)
	callCount := 0
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp completion.Response
		if callCount == 1 {
			resp = completion.Response{Choices: []completion.Choice{{
				Message: completion.Message{Role: completion.RoleAssistant, ToolCalls: []completion.ToolCall{{
					ID: "c1", Type: "function", Function: completion.FunctionCall{Name: "greet", Arguments: `{"name":"X"}`},
				}}},
				FinishReason: "tool_calls",
			}}}
		} else {
			resp = completion.Response{Choices: []completion.Choice{{Message: completion.Message{Role: completion.RoleAssistant, Content: "done"}, FinishReason: "stop"}}}
		}
		json.NewEncoder(w).Encode(resp)
	})

	if _, err := agent.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var turn, tool *tracetest.SpanStub
	for i := range exp.GetSpans() {
		s := &exp.GetSpans()[i]
		switch s.Name {
		case "agent.turn":
			turn = s
		case "tool.call":
			tool = s
		}
	}
	if turn == nil {
		t.Fatal("agent.turn span missing")
	}
	if tool == nil {
		t.Fatal("tool.call span missing")
	}
	// Nesting: tool.call's parent SpanID should match agent.turn's SpanID.
	if tool.Parent.SpanID() != turn.SpanContext.SpanID() {
		t.Errorf("tool.call parent=%s, want agent.turn span=%s", tool.Parent.SpanID(), turn.SpanContext.SpanID())
	}
	if got := attrLookupInt(turn.Attributes, "turn.tool_calls"); got != 1 {
		t.Errorf("turn.tool_calls: got %d want 1", got)
	}
	if got := attrLookupInt(turn.Attributes, "turn.iterations"); got != 2 {
		t.Errorf("turn.iterations: got %d want 2", got)
	}
	if got := attrLookupString(tool.Attributes, "tool.name"); got != "greet" {
		t.Errorf("tool.name: got %q want greet", got)
	}
}

func TestRun_EmitsAgentTurnSpan_RecordsErrorOnHookBlock(t *testing.T) {
	exp := newAgentSpanRecorder(t)
	agent := setupTestAgent(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called — turn.start hook blocks first.
	})
	// Block via turn.start hook: import via agent.Hooks() — done inline below.
	blockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer blockServer.Close()

	agent.Hooks().Register(hookBlockTurnStart())

	if _, err := agent.Run(context.Background(), "hi"); err == nil {
		t.Fatal("expected blocked error")
	}

	var turn *tracetest.SpanStub
	for i := range exp.GetSpans() {
		if exp.GetSpans()[i].Name == "agent.turn" {
			turn = &exp.GetSpans()[i]
		}
	}
	if turn == nil {
		t.Fatal("agent.turn span missing")
	}
	if turn.Status.Code != otelcodes.Error {
		t.Errorf("status: got %v want Error", turn.Status.Code)
	}
}
