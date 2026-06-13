package harness

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tracing environment variables.
//
//	HARNESS_OTEL_ENDPOINT     = OTLP/HTTP traces endpoint (e.g. http://localhost:4318)
//	                            When unset, tracing is DISABLED (noop tracer).
//	HARNESS_OTEL_SAMPLE_RATIO = 0.0 .. 1.0 (default 1.0)
//	HARNESS_OTEL_SERVICE_NAME = service.name resource attribute (default "ai-harness")
//	HARNESS_OTEL_PROTOCOL     = http (default) | grpc (grpc reserved for v2)
//
// Phase 5.2: OpenTelemetry tracing as a runtime contract. Pi has no first-class
// tracing model; Copilot CLI extensions have no harness-level provenance. Every
// turn, delegation, and tool call eventually emits an OTel span (instrumentation
// points wired in PR B). PR A (this file) installs the tracer provider, env/flag
// plumbing, exporter wiring, and an slog handler middleware that injects
// trace_id / span_id into every record.
const (
	EnvOtelEndpoint    = "HARNESS_OTEL_ENDPOINT"
	EnvOtelSampleRatio = "HARNESS_OTEL_SAMPLE_RATIO"
	EnvOtelServiceName = "HARNESS_OTEL_SERVICE_NAME"
	EnvOtelProtocol    = "HARNESS_OTEL_PROTOCOL"

	defaultOtelServiceName = "ai-harness"
	defaultOtelProtocol    = "http"
	tracerInstrumentation  = "github.com/htekdev/ai-harness"
)

var (
	tracerMu       sync.RWMutex
	tracerProvider trace.TracerProvider
	tracerShutdown func(context.Context) error
)

// Tracer returns the package-wide OTel tracer. When tracing is disabled
// (no HARNESS_OTEL_ENDPOINT, no SetTracerProvider), it returns a noop tracer
// with zero overhead. Always non-nil and safe for concurrent use.
func Tracer() trace.Tracer {
	tracerMu.RLock()
	tp := tracerProvider
	tracerMu.RUnlock()
	if tp == nil {
		return noop.NewTracerProvider().Tracer(tracerInstrumentation)
	}
	return tp.Tracer(tracerInstrumentation)
}

// SetTracerProvider installs the process-wide tracer provider and registers
// it as the OTel global. Pass nil to revert to the noop tracer (used by tests
// to reset state). The optional shutdown function (e.g. provider.Shutdown) is
// stored so ShutdownTracer can flush pending spans on process exit.
func SetTracerProvider(tp trace.TracerProvider, shutdown func(context.Context) error) {
	tracerMu.Lock()
	defer tracerMu.Unlock()
	tracerProvider = tp
	tracerShutdown = shutdown
	if tp != nil {
		otel.SetTracerProvider(tp)
	} else {
		otel.SetTracerProvider(noop.NewTracerProvider())
	}
}

// ShutdownTracer flushes pending spans and tears down the exporter. Safe to
// call when tracing was never initialized — returns nil in that case. Should
// be deferred from main() so SIGINT-driven exits flush the OTLP queue.
func ShutdownTracer(ctx context.Context) error {
	tracerMu.RLock()
	fn := tracerShutdown
	tracerMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

// ConfigureTracerFromFlags resolves CLI flags + env vars, builds the tracer
// provider, and installs it globally. Empty arguments mean "use the env var".
// When the resolved endpoint is empty, tracing stays disabled and the function
// returns nil (no error) — disabled-by-default is the documented behavior.
func ConfigureTracerFromFlags(endpoint, sampleRatio, serviceName string) error {
	endpoint = firstNonEmpty(endpoint, os.Getenv(EnvOtelEndpoint))
	sampleRatio = firstNonEmpty(sampleRatio, os.Getenv(EnvOtelSampleRatio))
	serviceName = firstNonEmpty(serviceName, os.Getenv(EnvOtelServiceName), defaultOtelServiceName)
	protocol := firstNonEmpty(os.Getenv(EnvOtelProtocol), defaultOtelProtocol)

	if endpoint == "" {
		// Disabled. Reset any previously-installed provider so tests that
		// toggle env vars between cases get a clean slate.
		SetTracerProvider(nil, nil)
		return nil
	}

	if protocol != "http" {
		return fmt.Errorf("invalid %s=%q (only \"http\" is supported in v1)", EnvOtelProtocol, protocol)
	}

	ratio, err := parseSampleRatio(sampleRatio)
	if err != nil {
		return err
	}

	exp, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpointURL(endpoint),
	)
	if err != nil {
		return fmt.Errorf("otlp http exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return fmt.Errorf("otel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(ratio)),
		sdktrace.WithResource(res),
	)
	SetTracerProvider(tp, tp.Shutdown)
	return nil
}

func parseSampleRatio(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1.0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q (want float in [0,1])", EnvOtelSampleRatio, s)
	}
	if v < 0 || v > 1 {
		return 0, fmt.Errorf("invalid %s=%q (out of range [0,1])", EnvOtelSampleRatio, s)
	}
	return v, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// TraceContextHandler wraps a slog.Handler and injects trace_id + span_id
// attrs from the record's context whenever a SpanContext is present. When no
// span is in flight, the handler is a transparent pass-through. Wired by
// NewLoggerWithTrace so every Phase-5.1 log record auto-carries trace IDs
// once Phase 5.2 instrumentation lands in PR B.
type TraceContextHandler struct {
	inner slog.Handler
}

// NewTraceContextHandler wraps inner so every Handle call injects trace
// correlation attrs when the context has a recording SpanContext.
func NewTraceContextHandler(inner slog.Handler) *TraceContextHandler {
	return &TraceContextHandler{inner: inner}
}

func (h *TraceContextHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *TraceContextHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *TraceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceContextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *TraceContextHandler) WithGroup(name string) slog.Handler {
	return &TraceContextHandler{inner: h.inner.WithGroup(name)}
}
