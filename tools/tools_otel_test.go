package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	otelcodes "go.opentelemetry.io/otel/codes"
)

// Phase 5.2 PR-B: span instrumentation for Registry.Execute.

func newSpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
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

func TestExecute_EmitsSpan_Success(t *testing.T) {
	exp := newSpanRecorder(t)
	reg := NewRegistry()
	if err := reg.Register(Definition{Name: "echo"}, echoHandler); err != nil {
		t.Fatalf("register: %v", err)
	}
	res := reg.Execute(context.Background(), Call{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{}`)})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Name != "tool.call" {
		t.Errorf("span name: got %q want tool.call", s.Name)
	}
	if got := attrString(s.Attributes, "tool.name"); got != "echo" {
		t.Errorf("tool.name: got %q want echo", got)
	}
	if got := attrString(s.Attributes, "tool.call_id"); got != "c1" {
		t.Errorf("tool.call_id: got %q want c1", got)
	}
	if got := attrBool(s.Attributes, "tool.is_error"); got {
		t.Errorf("tool.is_error: got true want false")
	}
	if s.Status.Code == otelcodes.Error {
		t.Errorf("status: unexpected Error on success")
	}
}

func TestExecute_EmitsSpan_NotFound(t *testing.T) {
	exp := newSpanRecorder(t)
	reg := NewRegistry()
	res := reg.Execute(context.Background(), Call{ID: "c1", Name: "missing"})
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	s := spans[0]
	if !attrBool(s.Attributes, "tool.is_error") {
		t.Errorf("tool.is_error should be true for not-found")
	}
	if s.Status.Code != otelcodes.Error {
		t.Errorf("status code: got %v want Error", s.Status.Code)
	}
	if !strings.Contains(s.Status.Description, "not found") {
		t.Errorf("status desc: got %q want substring 'not found'", s.Status.Description)
	}
}

func TestExecute_EmitsSpan_HandlerError(t *testing.T) {
	exp := newSpanRecorder(t)
	reg := NewRegistry()
	boomErr := errors.New("boom")
	_ = reg.Register(Definition{Name: "fail"}, func(ctx context.Context, args json.RawMessage) (string, error) {
		return "", boomErr
	})
	res := reg.Execute(context.Background(), Call{ID: "c2", Name: "fail"})
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Status.Code != otelcodes.Error {
		t.Errorf("status code: got %v want Error", s.Status.Code)
	}
	// span.RecordError adds an exception event
	foundException := false
	for _, ev := range s.Events {
		if ev.Name == "exception" {
			foundException = true
		}
	}
	if !foundException {
		t.Errorf("expected exception event recorded for handler error")
	}
}
