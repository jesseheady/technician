package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

var tracer trace.Tracer

func InitOTEL(ctx context.Context, cfg *config.OTELConfig, serviceName string) (func(context.Context) error, error) {
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	// A scheme-qualified endpoint decides its own transport: http:// exports in
	// plaintext (the usual local/sidecar collector), https:// over TLS. A bare
	// "host:port" keeps the OTel default of TLS rather than silently
	// downgrading traces onto the wire unencrypted.
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
	if strings.Contains(cfg.Endpoint, "://") {
		u, err := url.Parse(cfg.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("parsing OTLP endpoint: %w", err)
		}
		// Not WithEndpointURL: since otlptracehttp v1.45.0 a path-less URL there
		// posts to "/" instead of the default /v1/traces.
		opts = []otlptracehttp.Option{otlptracehttp.WithEndpoint(u.Host)}
		if u.Path != "" && u.Path != "/" {
			opts = append(opts, otlptracehttp.WithURLPath(u.Path))
		}
		if u.Scheme != "https" {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	tracer = tp.Tracer("technician")

	slog.Info("OTLP tracing initialized", "endpoint", cfg.Endpoint)

	return tp.Shutdown, nil
}

// TraceCheckResult emits an OTLP span for a check result and returns the span's
// trace and span IDs (empty when tracing is disabled) so the caller can stamp
// them onto the corresponding log line for Loki↔trace correlation.
func TraceCheckResult(ctx context.Context, result *check.Result) (traceID, spanID string) {
	if tracer == nil {
		return "", ""
	}

	// Spans are emitted after the check has already run, so anchor them to when
	// the check actually happened. Without explicit timestamps every span would
	// start and end at drain time, collapsing to ~0 duration in the trace UI.
	start := result.Timestamp
	if start.IsZero() {
		start = time.Now().Add(-result.Duration)
	}

	_, span := tracer.Start(ctx, fmt.Sprintf("check.%s.%s", result.Type, result.Name),
		trace.WithTimestamp(start),
		trace.WithAttributes(
			attribute.String("check.type", string(result.Type)),
			attribute.String("check.name", result.Name),
			attribute.Bool("check.success", result.Success),
			attribute.Float64("check.duration_ms", float64(result.Duration.Milliseconds())),
		),
	)
	defer span.End(trace.WithTimestamp(start.Add(result.Duration)))

	sc := span.SpanContext()
	traceID, spanID = sc.TraceID().String(), sc.SpanID().String()

	for k, v := range result.Labels {
		span.SetAttributes(attribute.String(k, v))
	}

	if result.Error != "" {
		span.SetStatus(codes.Error, result.Error)
		span.SetAttributes(attribute.String("check.error", result.Error))
	} else {
		span.SetStatus(codes.Ok, "")
	}

	if result.StatusCode > 0 {
		span.SetAttributes(attribute.Int("http.status_code", result.StatusCode))
	}

	if result.WebVitals != nil {
		v := result.WebVitals
		span.SetAttributes(
			attribute.Float64("browser.ttfb_ms", v.TTFB),
			attribute.Float64("browser.fcp_ms", v.FCP),
			attribute.Float64("browser.lcp_ms", v.LCP),
			attribute.Float64("browser.cls", v.CLS),
			attribute.Float64("browser.inp_ms", v.INP),
			attribute.Float64("browser.dom_complete_ms", v.DOMComplete),
		)
	}

	// Add HAR entries as span events
	if result.HARData != nil {
		for _, entry := range result.HARData.Entries {
			span.AddEvent("har.resource",
				trace.WithAttributes(
					attribute.String("url", entry.URL),
					attribute.String("resource_type", entry.ResourceType),
					attribute.Float64("duration_ms", entry.Duration),
					attribute.Int64("transfer_size", entry.TransferSize),
					attribute.Int("status", entry.Status),
				),
			)
		}
	}

	return traceID, spanID
}
