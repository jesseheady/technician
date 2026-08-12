// Package remotewrite pushes technician_* metrics to a Prometheus-compatible
// remote-write endpoint (AMP, Grafana Cloud, Mimir, Thanos) on a timer, for
// deployments Prometheus can't scrape.
//
// It encodes the remote-write 1.0 protobuf (snappy-compressed) directly with
// google.golang.org/protobuf/encoding/protowire instead of pulling the large
// prometheus/prometheus client. Because the exported metrics are gauges — a
// last-value snapshot — delivery is best-effort: a failed push is logged and
// dropped, and the next tick resends current state, so there is no WAL or queue.
package remotewrite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/protowire"
)

const metricPrefix = "technician_"

// Options configures a Writer.
type Options struct {
	URL      string
	Interval time.Duration
	SigV4    bool
	Region   string
	Headers  map[string]string
	Gatherer prometheus.Gatherer // defaults to prometheus.DefaultGatherer
}

// Writer periodically pushes a snapshot of the technician_* metrics.
type Writer struct {
	url      string
	interval time.Duration
	headers  map[string]string
	gatherer prometheus.Gatherer
	client   *http.Client

	// SigV4 (AMP). A nil signer means requests are sent unsigned.
	signer *v4.Signer
	creds  aws.CredentialsProvider
	region string
}

// New validates config and, when SigV4 is enabled, loads the AWS credential
// chain up front so a misconfiguration fails at startup rather than every tick.
func New(ctx context.Context, opts Options) (*Writer, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("remote-write URL is empty")
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	g := opts.Gatherer
	if g == nil {
		g = prometheus.DefaultGatherer
	}
	w := &Writer{
		url:      opts.URL,
		interval: interval,
		headers:  opts.Headers,
		gatherer: g,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
	if opts.SigV4 {
		var loadOpts []func(*awsconfig.LoadOptions) error
		if opts.Region != "" {
			loadOpts = append(loadOpts, awsconfig.WithRegion(opts.Region))
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
		if err != nil {
			return nil, fmt.Errorf("loading AWS config for SigV4: %w", err)
		}
		if awsCfg.Region == "" {
			return nil, fmt.Errorf("remote_write_sigv4 needs a region (set remote_write_region or AWS_REGION)")
		}
		w.signer = v4.NewSigner()
		w.creds = awsCfg.Credentials
		w.region = awsCfg.Region
	}
	return w, nil
}

// Run pushes on each interval tick until ctx is cancelled.
func (w *Writer) Run(ctx context.Context) {
	slog.Info("Prometheus remote-write started", "url", w.url, "interval", w.interval, "sigv4", w.signer != nil)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.push(ctx); err != nil {
				slog.Warn("remote-write push failed", "error", err)
			}
		}
	}
}

func (w *Writer) push(ctx context.Context) error {
	mfs, err := w.gatherer.Gather()
	if err != nil {
		return fmt.Errorf("gathering metrics: %w", err)
	}
	payload := encodeWriteRequest(mfs, time.Now().UnixMilli())
	if payload == nil {
		return nil // nothing recorded yet
	}
	compressed := snappy.Encode(nil, payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(compressed))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("User-Agent", "technician-remote-write")
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}
	if w.signer != nil {
		creds, err := w.creds.Retrieve(ctx)
		if err != nil {
			return fmt.Errorf("retrieving AWS credentials: %w", err)
		}
		sum := sha256.Sum256(compressed)
		if err := w.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), "aps", w.region, time.Now()); err != nil {
			return fmt.Errorf("signing request: %w", err)
		}
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("remote-write endpoint returned %s", resp.Status)
	}
	return nil
}

// encodeWriteRequest builds an (uncompressed) remote-write WriteRequest from the
// technician_* families. Returns nil when there is nothing to send.
func encodeWriteRequest(mfs []*dto.MetricFamily, tsMillis int64) []byte {
	var out []byte
	for _, mf := range mfs {
		name := mf.GetName()
		if !strings.HasPrefix(name, metricPrefix) {
			continue
		}
		for _, m := range mf.GetMetric() {
			val, ok := sampleValue(mf.GetType(), m)
			if !ok {
				continue
			}
			series := encodeTimeSeries(name, m.GetLabel(), val, tsMillis)
			out = protowire.AppendTag(out, 1, protowire.BytesType) // WriteRequest.timeseries
			out = protowire.AppendBytes(out, series)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sampleValue(t dto.MetricType, m *dto.Metric) (float64, bool) {
	switch t {
	case dto.MetricType_GAUGE:
		return m.GetGauge().GetValue(), true
	case dto.MetricType_COUNTER:
		return m.GetCounter().GetValue(), true
	case dto.MetricType_UNTYPED:
		return m.GetUntyped().GetValue(), true
	default:
		return 0, false // histograms/summaries: none exported today
	}
}

func encodeTimeSeries(name string, labels []*dto.LabelPair, value float64, tsMillis int64) []byte {
	// __name__ plus the metric's labels, sorted by name (remote-write requires
	// labels in sorted order).
	type kv struct{ k, v string }
	pairs := make([]kv, 0, len(labels)+1)
	pairs = append(pairs, kv{"__name__", name})
	for _, l := range labels {
		pairs = append(pairs, kv{l.GetName(), l.GetValue()})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })

	var ts []byte
	for _, p := range pairs {
		ts = protowire.AppendTag(ts, 1, protowire.BytesType) // TimeSeries.labels
		ts = protowire.AppendBytes(ts, encodeLabel(p.k, p.v))
	}
	ts = protowire.AppendTag(ts, 2, protowire.BytesType) // TimeSeries.samples
	ts = protowire.AppendBytes(ts, encodeSample(value, tsMillis))
	return ts
}

func encodeLabel(name, value string) []byte {
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.BytesType)
	b = protowire.AppendString(b, name)
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	b = protowire.AppendString(b, value)
	return b
}

func encodeSample(value float64, tsMillis int64) []byte {
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.Fixed64Type) // Sample.value (double)
	b = protowire.AppendFixed64(b, math.Float64bits(value))
	b = protowire.AppendTag(b, 2, protowire.VarintType) // Sample.timestamp (int64)
	b = protowire.AppendVarint(b, uint64(tsMillis))
	return b
}
