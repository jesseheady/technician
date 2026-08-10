package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

// withRecordingTracer swaps in an in-memory tracer for the duration of a test
// and returns the exporter holding the spans it produced.
func withRecordingTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp)))

	prev := tracer
	tracer = tp.Tracer("test")
	t.Cleanup(func() {
		tracer = prev
		_ = tp.Shutdown(context.Background())
	})
	return exp
}

func attrsOf(t *testing.T, span tracetest.SpanStub) map[string]any {
	t.Helper()
	out := make(map[string]any, len(span.Attributes))
	for _, kv := range span.Attributes {
		out[string(kv.Key)] = kv.Value.AsInterface()
	}
	return out
}

// TestTraceCheckResultSpanTiming pins the property that spans are anchored to
// when the check ran, not when the result was drained. Without explicit
// timestamps the span would collapse to ~0 duration.
func TestTraceCheckResultSpanTiming(t *testing.T) {
	exp := withRecordingTracer(t)

	ranAt := time.Now().Add(-30 * time.Second)
	r := check.NewResult("timed-check", config.CheckTypeHTTP, nil)
	r.Success = true
	r.Timestamp = ranAt
	r.Duration = 250 * time.Millisecond

	TraceCheckResult(context.Background(), r)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]

	if got := span.EndTime.Sub(span.StartTime); got != 250*time.Millisecond {
		t.Errorf("expected span duration to match check duration 250ms, got %v", got)
	}
	if !span.StartTime.Equal(ranAt) {
		t.Errorf("expected span to start when the check ran (%v), got %v", ranAt, span.StartTime)
	}
	if span.Name != "check.http.timed-check" {
		t.Errorf("unexpected span name: %q", span.Name)
	}

	attrs := attrsOf(t, span)
	if attrs["check.name"] != "timed-check" || attrs["check.success"] != true {
		t.Errorf("unexpected span attributes: %v", attrs)
	}
}

// TestTraceCheckResultZeroTimestamp covers results that carry a duration but no
// timestamp: the span must still span the check's duration rather than nothing.
func TestTraceCheckResultZeroTimestamp(t *testing.T) {
	exp := withRecordingTracer(t)

	r := check.NewResult("no-timestamp", config.CheckTypeTCP, nil)
	r.Success = true
	r.Duration = 100 * time.Millisecond
	r.Timestamp = time.Time{}

	TraceCheckResult(context.Background(), r)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].EndTime.Sub(spans[0].StartTime); got != 100*time.Millisecond {
		t.Errorf("expected 100ms span for a result with no timestamp, got %v", got)
	}
}

// TestTraceCheckResultRecordsFailure verifies a failed check is marked as an
// error span, which is what makes traces useful for triage.
func TestTraceCheckResultRecordsFailure(t *testing.T) {
	exp := withRecordingTracer(t)

	r := check.NewResult("failing-check", config.CheckTypeHTTP, nil)
	r.Success = false
	r.Error = "connection refused"
	r.Timestamp = time.Now()
	r.Duration = time.Second

	TraceCheckResult(context.Background(), r)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("expected error status on a failed check, got %v", spans[0].Status.Code)
	}
	if attrs := attrsOf(t, spans[0]); attrs["check.error"] != "connection refused" {
		t.Errorf("expected check.error attribute, got %v", attrs)
	}
}

// TestTraceCheckResultNoTracer guards the disabled path: with no endpoint
// configured, tracing must be an inert no-op rather than a nil-pointer panic,
// and it must return empty IDs so the caller omits trace_id from the log line.
func TestTraceCheckResultNoTracer(t *testing.T) {
	prev := tracer
	tracer = nil
	t.Cleanup(func() { tracer = prev })

	r := check.NewResult("untraced", config.CheckTypeHTTP, nil)
	r.Success = true
	traceID, spanID := TraceCheckResult(context.Background(), r) // must not panic
	if traceID != "" || spanID != "" {
		t.Errorf("expected empty IDs when tracing is disabled, got %q/%q", traceID, spanID)
	}
}

// TestTraceCheckResultReturnsIDs pins the correlation contract: the returned
// trace/span IDs identify the emitted span so a log line can link to its trace.
func TestTraceCheckResultReturnsIDs(t *testing.T) {
	exp := withRecordingTracer(t)

	r := check.NewResult("correlated", config.CheckTypeHTTP, nil)
	r.Success = true
	r.Timestamp = time.Now()
	r.Duration = 10 * time.Millisecond

	traceID, spanID := TraceCheckResult(context.Background(), r)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if traceID != spans[0].SpanContext.TraceID().String() {
		t.Errorf("returned trace_id %q does not match span %q", traceID, spans[0].SpanContext.TraceID())
	}
	if spanID != spans[0].SpanContext.SpanID().String() {
		t.Errorf("returned span_id %q does not match span %q", spanID, spans[0].SpanContext.SpanID())
	}
	if traceID == "" || spanID == "" {
		t.Error("expected non-empty IDs when tracing is enabled")
	}
}

// TestInitOTELExportsToPlaintextCollector is the end-to-end path: an http://
// endpoint must actually deliver spans to a plaintext collector. Without
// scheme handling the exporter defaults to TLS and posts to https://, which no
// plaintext collector (the usual local/sidecar setup) can answer.
func TestInitOTELExportsToPlaintextCollector(t *testing.T) {
	received := make(chan string, 4)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	prev := tracer
	t.Cleanup(func() { tracer = prev })

	// collector.URL is "http://127.0.0.1:PORT" — the scheme must select plaintext.
	shutdown, err := InitOTEL(context.Background(), &config.OTELConfig{Endpoint: collector.URL}, "test-service")
	if err != nil {
		t.Fatalf("InitOTEL: %v", err)
	}
	if tracer == nil {
		t.Fatal("expected tracer to be initialized for a configured endpoint")
	}

	r := check.NewResult("exported-check", config.CheckTypeHTTP, nil)
	r.Success = true
	r.Timestamp = time.Now()
	r.Duration = 5 * time.Millisecond
	TraceCheckResult(context.Background(), r)

	// Shutdown flushes the batcher.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown/flush: %v", err)
	}

	select {
	case path := <-received:
		if path != "/v1/traces" {
			t.Errorf("expected spans posted to /v1/traces, got %q", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collector received no spans — export never reached the endpoint")
	}
}

// TestInitOTELDisabledByDefault verifies an empty endpoint leaves tracing off
// and still returns a usable shutdown func.
func TestInitOTELDisabledByDefault(t *testing.T) {
	prev := tracer
	tracer = nil
	t.Cleanup(func() { tracer = prev })

	shutdown, err := InitOTEL(context.Background(), &config.OTELConfig{}, "test-service")
	if err != nil {
		t.Fatalf("expected no error for empty endpoint, got %v", err)
	}
	if tracer != nil {
		t.Error("expected tracer to stay nil when no endpoint is configured")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("expected no-op shutdown to succeed, got %v", err)
	}
}
