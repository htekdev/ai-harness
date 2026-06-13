package harness

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newSyncTracerProvider wires the in-memory exporter through a SimpleSpanProcessor
// so tests see spans synchronously on End. Lives in its own file so production
// builds don't pull tracetest into the binary unless test code is imported.
func newSyncTracerProvider(exp *tracetest.InMemoryExporter) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp)),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
}
