package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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
