package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestBGPCheckerType(t *testing.T) {
	checker := NewBGPChecker()
	if checker.Type() != config.CheckTypeBGP {
		t.Errorf("expected type %q, got %q", config.CheckTypeBGP, checker.Type())
	}
}

func TestBGPCheckerMissingConfig(t *testing.T) {
	checker := NewBGPChecker()
	cfg := &config.CheckConfig{
		Name: "test-bgp-nil",
		Type: config.CheckTypeBGP,
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil BGP config")
	}
	if result.Error != "missing BGP check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestBGPCheckerCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	checker := NewBGPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-bgp-cancelled",
		Type:    config.CheckTypeBGP,
		Timeout: 5 * time.Second,
		BGP: &config.BGPCheckConfig{
			Prefix:         "203.0.113.0/24",
			ExpectedOrigin: 64496,
		},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(ctx, cfg, origin)

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
