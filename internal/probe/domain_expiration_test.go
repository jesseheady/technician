package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

func TestDomainExpirationProberType(t *testing.T) {
	prober := NewDomainExpirationProber()
	if prober.Type() != config.ProbeTypeDomainExpiry {
		t.Errorf("expected type %q, got %q", config.ProbeTypeDomainExpiry, prober.Type())
	}
}

func TestDomainExpirationProberMissingConfig(t *testing.T) {
	prober := NewDomainExpirationProber()
	cfg := &config.ProbeConfig{
		Name: "test-domain-expiry-nil",
		Type: config.ProbeTypeDomainExpiry,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil DomainExpiry config")
	}
	if result.Error != "missing domain expiration probe configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestDomainExpirationProberCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	prober := NewDomainExpirationProber()
	cfg := &config.ProbeConfig{
		Name:    "test-domain-expiry-cancelled",
		Type:    config.ProbeTypeDomainExpiry,
		Timeout: 5 * time.Second,
		DomainExpiry: &config.DomainExpirationProbeConfig{
			Domain:       "example.com",
			WarnDays:     30,
			CriticalDays: 7,
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
	if !strings.Contains(result.Error, "RDAP query failed") {
		t.Errorf("expected error to mention RDAP query failure, got: %s", result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}
