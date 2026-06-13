package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func resetTracerForTest(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { SetTracerProvider(nil, nil) })
	SetTracerProvider(nil, nil)
}

func TestTracer_DefaultIsNoop(t *testing.T) {
	resetTracerForTest(t)
	tr := Tracer()
	if tr == nil {
		t.Fatal("Tracer() returned nil")
	}
	_, span := tr.Start(context.Background(), "noop-span")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Fatal("noop tracer must not produce a valid SpanContext")
	}
}

func TestConfigureTracerFromFlags_Disabled(t *testing.T) {
	resetTracerForTest(t)
	t.Setenv(EnvOtelEndpoint, "")
	if err := ConfigureTracerFromFlags("", "", ""); err != nil {
		t.Fatalf("expected nil err with no endpoint, got %v", err)
	}
	// Tracer must remain noop.
	_, span := Tracer().Start(context.Background(), "still-noop")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Fatal("expected noop tracer when endpoint is empty")
	}
}

func TestConfigureTracerFromFlags_BadProtocol(t *testing.T) {
	resetTracerForTest(t)
	t.Setenv(EnvOtelProtocol, "grpc")
	err := ConfigureTracerFromFlags("http://localhost:4318", "1.0", "test")
	if err == nil || !strings.Contains(err.Error(), "only \"http\"") {
		t.Fatalf("expected protocol error, got %v", err)
	}
}

func TestConfigureTracerFromFlags_BadSampleRatio(t *testing.T) {
	resetTracerForTest(t)
	cases := []string{"abc", "-0.1", "1.5"}
	for _, c := range cases {
		err := ConfigureTracerFromFlags("http://localhost:4318", c, "test")
		if err == nil {
			t.Fatalf("ratio %q: expected error", c)
		}
	}
}

func TestConfigureTracerFromFlags_GoodEndpoint(t *testing.T) {
	resetTracerForTest(t)
	// Use a clearly invalid host but valid URL — exporter creation should
	// still succeed (it lazy-connects on first export).
	err := ConfigureTracerFromFlags("http://127.0.0.1:14318", "0.5", "test-svc")
	if err != nil {
		t.Fatalf("ConfigureTracerFromFlags: %v", err)
	}
	if err := ShutdownTracer(context.Background()); err != nil {
		// Shutdown over a dead endpoint should still succeed; the SDK
		// drains its in-memory queue without requiring exporter success.
		t.Logf("shutdown returned %v (acceptable for unreachable endpoint)", err)
	}
}

func TestTraceContextHandler_InjectsIDsWhenSpanPresent(t *testing.T) {
	resetTracerForTest(t)
	exp := tracetest.NewInMemoryExporter()
	tp := newSyncTracerProvider(exp)
	SetTracerProvider(tp, tp.Shutdown)

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(NewTraceContextHandler(inner))

	ctx, span := Tracer().Start(context.Background(), "test")
	logger.InfoContext(ctx, "hello")
	span.End()

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("decode log: %v (raw=%q)", err, buf.String())
	}
	tid, _ := rec["trace_id"].(string)
	sid, _ := rec["span_id"].(string)
	wantTID := span.SpanContext().TraceID().String()
	wantSID := span.SpanContext().SpanID().String()
	if tid != wantTID {
		t.Errorf("trace_id: got %q want %q", tid, wantTID)
	}
	if sid != wantSID {
		t.Errorf("span_id: got %q want %q", sid, wantSID)
	}
}

func TestTraceContextHandler_NoOpWhenNoSpan(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(NewTraceContextHandler(inner))

	logger.InfoContext(context.Background(), "no-span")
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if _, ok := rec["trace_id"]; ok {
		t.Error("trace_id must not be set when no span in context")
	}
	if _, ok := rec["span_id"]; ok {
		t.Error("span_id must not be set when no span in context")
	}
}

func TestTraceContextHandler_PassesEnabledAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewTraceContextHandler(inner)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug must be filtered when inner level is warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("error must pass through")
	}

	withAttrs := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if _, ok := withAttrs.(*TraceContextHandler); !ok {
		t.Errorf("WithAttrs should return *TraceContextHandler, got %T", withAttrs)
	}
	withGroup := h.WithGroup("g")
	if _, ok := withGroup.(*TraceContextHandler); !ok {
		t.Errorf("WithGroup should return *TraceContextHandler, got %T", withGroup)
	}
}

func TestParseSampleRatio(t *testing.T) {
	cases := []struct {
		in      string
		want    float64
		wantErr bool
	}{
		{"", 1.0, false},
		{"0", 0, false},
		{"0.25", 0.25, false},
		{"1", 1, false},
		{"1.0001", 0, true},
		{"-0.0001", 0, true},
		{"abc", 0, true},
	}
	for _, c := range cases {
		got, err := parseSampleRatio(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseSampleRatio(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSampleRatio(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSampleRatio(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// (helper removed; use newSyncTracerProvider directly)
