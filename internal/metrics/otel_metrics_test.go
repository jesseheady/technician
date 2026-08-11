package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jesseheady/technician/internal/config"
)

func TestParseOTLPEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		wantHost     string
		wantPath     string
		wantInsecure bool
	}{
		{"bare host:port keeps TLS default", "collector:4318", "collector:4318", "", false},
		{"http is plaintext", "http://collector:4318", "collector:4318", "", true},
		{"https stays TLS", "https://collector:4318", "collector:4318", "", false},
		{"explicit path is preserved", "http://collector:4318/v1/metrics", "collector:4318", "/v1/metrics", true},
		{"root path is treated as default", "http://collector:4318/", "collector:4318", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep, err := parseOTLPEndpoint(tt.endpoint)
			if err != nil {
				t.Fatalf("parseOTLPEndpoint(%q): %v", tt.endpoint, err)
			}
			if ep.host != tt.wantHost || ep.path != tt.wantPath || ep.insecure != tt.wantInsecure {
				t.Errorf("parseOTLPEndpoint(%q) = %+v, want host=%q path=%q insecure=%v",
					tt.endpoint, ep, tt.wantHost, tt.wantPath, tt.wantInsecure)
			}
		})
	}
}

func TestPrefixGathererKeepsOnlyTechnicianFamilies(t *testing.T) {
	reg := prometheus.NewRegistry()
	techMetric := prometheus.NewGauge(prometheus.GaugeOpts{Name: "technician_check_healthy", Help: "test"})
	selfHealth := prometheus.NewGauge(prometheus.GaugeOpts{Name: "go_goroutines", Help: "test"})
	reg.MustRegister(techMetric, selfHealth)
	techMetric.Set(1)
	selfHealth.Set(42)

	mfs, err := prefixGatherer{g: reg, prefix: "technician_"}.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(mfs) != 1 {
		names := make([]string, len(mfs))
		for i, mf := range mfs {
			names[i] = mf.GetName()
		}
		t.Fatalf("kept %d families %v, want only technician_*", len(mfs), names)
	}
	if got := mfs[0].GetName(); got != "technician_check_healthy" {
		t.Errorf("kept %q, want technician_check_healthy", got)
	}
}

// TestInitOTELMetricsExportsToCollector is the end-to-end path: with metrics
// enabled, a technician_* sample must reach the collector's /v1/metrics on
// shutdown (which flushes the periodic reader).
func TestInitOTELMetricsExportsToCollector(t *testing.T) {
	received := make(chan string, 4)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "technician_test_export", Help: "test"})
	prometheus.MustRegister(g)
	t.Cleanup(func() { prometheus.Unregister(g) })
	g.Set(1)

	shutdown, err := InitOTELMetrics(context.Background(),
		&config.OTELConfig{Endpoint: collector.URL, Metrics: true}, "test-service")
	if err != nil {
		t.Fatalf("InitOTELMetrics: %v", err)
	}

	// Shutdown collects and exports a final time.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown/flush: %v", err)
	}

	select {
	case path := <-received:
		if path != "/v1/metrics" {
			t.Errorf("expected metrics posted to /v1/metrics, got %q", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collector received no metrics — export never reached the endpoint")
	}
}

// TestInitOTELMetricsDisabled covers the no-op paths: no endpoint, and an
// endpoint with metrics off (traces may run, metrics must not).
func TestInitOTELMetricsDisabled(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  config.OTELConfig
	}{
		{"no endpoint", config.OTELConfig{}},
		{"endpoint set but metrics off", config.OTELConfig{Endpoint: "http://127.0.0.1:4318", Metrics: false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			shutdown, err := InitOTELMetrics(context.Background(), &tt.cfg, "svc")
			if err != nil {
				t.Fatalf("InitOTELMetrics: %v", err)
			}
			if err := shutdown(context.Background()); err != nil {
				t.Fatalf("noop shutdown: %v", err)
			}
		})
	}
}
