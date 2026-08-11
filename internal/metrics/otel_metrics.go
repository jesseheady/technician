package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	otelprom "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/jesseheady/technician/internal/config"
)

// otlpEndpoint is the transport decision shared by the trace and metric
// exporters. A scheme-qualified endpoint picks its own transport (http://
// plaintext, https:// TLS); a bare "host:port" keeps the OTel TLS default
// rather than silently downgrading onto the wire unencrypted.
type otlpEndpoint struct {
	host     string
	path     string // empty => exporter default (/v1/traces, /v1/metrics)
	insecure bool
}

func parseOTLPEndpoint(endpoint string) (otlpEndpoint, error) {
	ep := otlpEndpoint{host: endpoint}
	if !strings.Contains(endpoint, "://") {
		return ep, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return otlpEndpoint{}, fmt.Errorf("parsing OTLP endpoint: %w", err)
	}
	ep.host = u.Host
	if u.Path != "" && u.Path != "/" {
		ep.path = u.Path
	}
	ep.insecure = u.Scheme != "https"
	return ep, nil
}

// InitOTELMetrics exports the technician_* Prometheus metrics via OTLP, in
// addition to the /metrics endpoint, when metrics.otel.metrics is set. It reuses
// the trace endpoint — a collector accepts traces and metrics on the same host,
// on different default paths. No-op when the endpoint is empty or metrics are
// off. The returned func flushes and shuts down the meter provider.
func InitOTELMetrics(ctx context.Context, cfg *config.OTELConfig, serviceName string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if cfg.Endpoint == "" || !cfg.Metrics {
		return noop, nil
	}

	ep, err := parseOTLPEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(ep.host)}
	if ep.path != "" {
		opts = append(opts, otlpmetrichttp.WithURLPath(ep.path))
	}
	if ep.insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)))
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	// Bridge our own Prometheus registry into the reader instead of declaring a
	// second set of instruments: the producer gathers the registry each export
	// cycle, so OTLP stays in parity with /metrics automatically. Filter to
	// technician_* so the stream carries check metrics, not the Go/process
	// self-health collectors (those stay on /metrics for Prometheus scraping).
	producer := otelprom.NewMetricProducer(
		otelprom.WithGatherer(prefixGatherer{g: prometheus.DefaultGatherer, prefix: "technician_"}),
	)
	reader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithProducer(producer))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
	otel.SetMeterProvider(mp)

	slog.Info("OTLP metrics initialized", "endpoint", cfg.Endpoint)
	return mp.Shutdown, nil
}

// prefixGatherer wraps a prometheus.Gatherer and returns only the metric
// families whose name starts with prefix.
type prefixGatherer struct {
	g      prometheus.Gatherer
	prefix string
}

func (pg prefixGatherer) Gather() ([]*dto.MetricFamily, error) {
	mfs, err := pg.g.Gather()
	if err != nil {
		return nil, err
	}
	kept := make([]*dto.MetricFamily, 0, len(mfs))
	for _, mf := range mfs {
		if strings.HasPrefix(mf.GetName(), pg.prefix) {
			kept = append(kept, mf)
		}
	}
	return kept, nil
}
