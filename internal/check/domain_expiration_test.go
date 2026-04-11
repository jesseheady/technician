package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

func TestDomainExpirationCheckerType(t *testing.T) {
	checker := NewDomainExpirationChecker()
	if checker.Type() != config.CheckTypeDomainExpiry {
		t.Errorf("expected type %q, got %q", config.CheckTypeDomainExpiry, checker.Type())
	}
}

func TestDomainExpirationCheckerMissingConfig(t *testing.T) {
	checker := NewDomainExpirationChecker()
	cfg := &config.CheckConfig{
		Name: "test-domain-expiry-nil",
		Type: config.CheckTypeDomainExpiry,
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil DomainExpiry config")
	}
	if result.Error != "missing domain expiration check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestDomainExpirationCheckerCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	checker := NewDomainExpirationChecker()
	cfg := &config.CheckConfig{
		Name:    "test-domain-expiry-cancelled",
		Type:    config.CheckTypeDomainExpiry,
		Timeout: 5 * time.Second,
		DomainExpiry: &config.DomainExpirationCheckConfig{
			Domain:       "example.com",
			WarnDays:     30,
			CriticalDays: 7,
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
	if !strings.Contains(result.Error, "RDAP query failed") {
		t.Errorf("expected error to mention RDAP query failure, got: %s", result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}
