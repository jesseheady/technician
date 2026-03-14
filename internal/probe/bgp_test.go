package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestBGPProberType(t *testing.T) {
	prober := NewBGPProber()
	if prober.Type() != config.ProbeTypeBGP {
		t.Errorf("expected type %q, got %q", config.ProbeTypeBGP, prober.Type())
	}
}

func TestBGPProberMissingConfig(t *testing.T) {
	prober := NewBGPProber()
	cfg := &config.ProbeConfig{
		Name: "test-bgp-nil",
		Type: config.ProbeTypeBGP,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil BGP config")
	}
	if result.Error != "missing BGP probe configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestBGPProberCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	prober := NewBGPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-bgp-cancelled",
		Type:    config.ProbeTypeBGP,
		Timeout: 5 * time.Second,
		BGP: &config.BGPProbeConfig{
			Prefix:         "203.0.113.0/24",
			ExpectedOrigin: 64496,
		},
	}
	site := &config.Site{Code: "test", City: "Test", Country: "XX"}

	result := prober.Run(ctx, cfg, site)

	if result.Success {
		t.Error("expected failure for cancelled context")
	}
	if !result.InfraError {
		t.Error("expected InfraError=true for cancelled context")
	}
	if !strings.Contains(result.Error, "RIPE Stat query failed") {
		t.Errorf("expected error to mention RIPE Stat query failure, got: %s", result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}
