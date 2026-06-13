package main

// Phase 5.2 PR-C: source.pump span instrumentation for runServe.
//
// Verifies that:
//   - Each Event read by a Source produces exactly one `source.pump` span.
//   - The span carries source.name / source.event.session_key /
//     source.event.text_len attributes.
//   - The downstream `agent.turn` span emitted by the injected runner is a
//     child of the matching `source.pump` span (so a Jaeger waterfall
//     shows the full path from input → completion).
//   - When the run() returns an error, the `source.pump` span records it
//     and is marked Error.

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/htekdev/ai-harness/agent"
	"github.com/htekdev/ai-harness/input"
)

// newServeSpanRecorder wires a SimpleSpanProcessor + InMemoryExporter into
// the OTel global so spans land synchronously on End. Returns the exporter
// and restores the previous provider on test cleanup.
func newServeSpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
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

// stringAttr looks up a span attribute by key and returns its string value.
func stringAttr(s tracetest.SpanStub, key string) string {
	for _, a := range s.Attributes {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

// intAttr looks up a span attribute by key and returns its int64 value.
func intAttr(s tracetest.SpanStub, key string) int64 {
	for _, a := range s.Attributes {
		if string(a.Key) == key {
			return a.Value.AsInt64()
		}
	}
	return 0
}

// TestRunServe_EmitsSourcePumpSpan verifies one source.pump span per event,
// with the documented attributes set.
func TestRunServe_EmitsSourcePumpSpan(t *testing.T) {
	exp := newServeSpanRecorder(t)

	src := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-42", Text: "ping"},
		},
	}

	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		return &agent.TurnResult{Response: "pong"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runServe(ctx, run, []input.Source{src}); err != nil {
		t.Fatalf("runServe: %v", err)
	}

	var pumps []tracetest.SpanStub
	for _, s := range exp.GetSpans() {
		if s.Name == "source.pump" {
			pumps = append(pumps, s)
		}
	}
	if len(pumps) != 1 {
		t.Fatalf("source.pump span count = %d, want 1; got spans = %v", len(pumps), exp.GetSpans())
	}
	pump := pumps[0]
	if got := stringAttr(pump, "source.name"); got != "telegram" {
		t.Errorf("source.name = %q, want telegram", got)
	}
	if got := stringAttr(pump, "source.event.session_key"); got != "chat-42" {
		t.Errorf("source.event.session_key = %q, want chat-42", got)
	}
	if got := intAttr(pump, "source.event.text_len"); got != 4 {
		t.Errorf("source.event.text_len = %d, want 4", got)
	}
}

// TestRunServe_AgentTurnNestsUnderSourcePump verifies that an `agent.turn`
// span emitted by the injected runner becomes a child of the `source.pump`
// span — i.e. parent linkage is preserved through serveJob.ctx.
func TestRunServe_AgentTurnNestsUnderSourcePump(t *testing.T) {
	exp := newServeSpanRecorder(t)

	src := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-X", Text: "hello"},
		},
	}

	// Runner emits a synthetic agent.turn span using the ctx passed in,
	// mirroring what agent.Run does in production.
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		_, span := otel.Tracer(serveTracerName).Start(ctx, "agent.turn")
		span.End()
		return &agent.TurnResult{Response: "ok"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runServe(ctx, run, []input.Source{src}); err != nil {
		t.Fatalf("runServe: %v", err)
	}

	var pump, turn *tracetest.SpanStub
	for i := range exp.GetSpans() {
		s := &exp.GetSpans()[i]
		switch s.Name {
		case "source.pump":
			pump = s
		case "agent.turn":
			turn = s
		}
	}
	if pump == nil {
		t.Fatalf("no source.pump span emitted; spans = %v", exp.GetSpans())
	}
	if turn == nil {
		t.Fatalf("no agent.turn span emitted; spans = %v", exp.GetSpans())
	}
	if turn.Parent.SpanID() != pump.SpanContext.SpanID() {
		t.Errorf("agent.turn parent = %v, want pump span = %v",
			turn.Parent.SpanID(), pump.SpanContext.SpanID())
	}
	if turn.SpanContext.TraceID() != pump.SpanContext.TraceID() {
		t.Errorf("traceIDs differ: turn=%v pump=%v",
			turn.SpanContext.TraceID(), pump.SpanContext.TraceID())
	}
}

// TestRunServe_SourcePumpRecordsRunError verifies the pump span captures
// runner errors via RecordError + Error status. This is what makes Jaeger
// waterfalls actionable for production debugging.
func TestRunServe_SourcePumpRecordsRunError(t *testing.T) {
	exp := newServeSpanRecorder(t)

	src := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-err", Text: "boom"},
		},
	}

	wantErr := errors.New("upstream completion 500")
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		return nil, wantErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runServe(ctx, run, []input.Source{src}); err != nil {
		t.Fatalf("runServe: %v", err)
	}

	var pump *tracetest.SpanStub
	for i := range exp.GetSpans() {
		if exp.GetSpans()[i].Name == "source.pump" {
			pump = &exp.GetSpans()[i]
			break
		}
	}
	if pump == nil {
		t.Fatalf("no source.pump span emitted")
	}
	if pump.Status.Code != otelcodes.Error {
		t.Errorf("status = %v, want Error", pump.Status.Code)
	}
	if pump.Status.Description != wantErr.Error() {
		t.Errorf("status description = %q, want %q", pump.Status.Description, wantErr.Error())
	}
	var sawErrorEvent bool
	for _, ev := range pump.Events {
		if ev.Name == "exception" {
			sawErrorEvent = true
			break
		}
	}
	if !sawErrorEvent {
		t.Errorf("no exception event recorded on pump span; events = %v", pump.Events)
	}
}

// TestRunServe_PumpSpansAreIndependentPerEvent guards against accidentally
// reusing a single span across events from one source: each event must get
// its own span (different span IDs, same trace IDs only if explicitly linked).
func TestRunServe_PumpSpansAreIndependentPerEvent(t *testing.T) {
	exp := newServeSpanRecorder(t)

	src := &fakeSource{
		name: "telegram",
		events: []input.Event{
			{SourceName: "telegram", SessionKey: "chat-A", Text: "one"},
			{SourceName: "telegram", SessionKey: "chat-A", Text: "two"},
			{SourceName: "telegram", SessionKey: "chat-B", Text: "three"},
		},
	}
	var runs int32
	run := func(ctx context.Context, text string) (*agent.TurnResult, error) {
		atomic.AddInt32(&runs, 1)
		return &agent.TurnResult{Response: "ack"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runServe(ctx, run, []input.Source{src}); err != nil {
		t.Fatalf("runServe: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 3 {
		t.Fatalf("runs = %d, want 3", got)
	}

	seen := map[string]struct{}{}
	for _, s := range exp.GetSpans() {
		if s.Name != "source.pump" {
			continue
		}
		id := s.SpanContext.SpanID().String()
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate pump span id %s", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != 3 {
		t.Errorf("unique pump spans = %d, want 3", len(seen))
	}
}

// Quiet unused-import guard for builds where io is dropped from the test
// file by accident — runServe imports it but tests don't reference it
// directly. This compiles into a no-op.
var _ = io.EOF
