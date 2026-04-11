package metrics

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/check"
)

var tracer trace.Tracer

func InitOTEL(ctx context.Context, cfg *config.OTELConfig, serviceName string) (func(context.Context) error, error) {
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	)
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

func TraceCheckResult(ctx context.Context, result *check.Result) {
	if tracer == nil {
		return
	}

	_, span := tracer.Start(ctx, fmt.Sprintf("check.%s.%s", result.Type, result.Name),
		trace.WithAttributes(
			attribute.String("check.type", string(result.Type)),
			attribute.String("check.name", result.Name),
			attribute.Bool("check.success", result.Success),
			attribute.Float64("check.duration_ms", float64(result.Duration.Milliseconds())),
		),
	)
	defer span.End()

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
}
