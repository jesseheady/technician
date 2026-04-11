package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestDNSCheckerARecord(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-a-record",
		Type:    config.CheckTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "google.com",
			Server:     "8.8.8.8:53",
			RecordType: "A",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if len(result.DNSAnswers) == 0 {
		t.Error("expected non-empty DNSAnswers")
	}
	if result.DNSQueryTime == 0 {
		t.Error("expected non-zero DNSQueryTime")
	}
}

func TestDNSCheckerAAAARecord(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-aaaa-record",
		Type:    config.CheckTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "google.com",
			Server:     "8.8.8.8:53",
			RecordType: "AAAA",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	for _, answer := range result.DNSAnswers {
		if !strings.Contains(answer, ":") {
			t.Errorf("expected IPv6 address containing ':', got %s", answer)
		}
	}
}

func TestDNSCheckerTXTRecord(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-txt-record",
		Type:    config.CheckTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "google.com",
			Server:     "8.8.8.8:53",
			RecordType: "TXT",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if len(result.DNSAnswers) == 0 {
		t.Error("expected non-empty answers for TXT record")
	}
}

func TestDNSCheckerExpectedMatch(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-expected-match",
		Type:    config.CheckTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "dns.google",
			Server:     "8.8.8.8:53",
			RecordType: "A",
			Expected:   []string{"8.8.8.8"},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestDNSCheckerExpectedMismatch(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-expected-mismatch",
		Type:    config.CheckTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "dns.google",
			Server:     "8.8.8.8:53",
			RecordType: "A",
			Expected:   []string{"1.2.3.4"},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for mismatched expected value")
	}
	if !strings.Contains(result.Error, "expected values not found") {
		t.Errorf("expected 'expected values not found' error, got: %s", result.Error)
	}
}

func TestDNSCheckerInvalidDomain(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-invalid-domain",
		Type:    config.CheckTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "thisdomain.doesnotexist.invalid",
			Server:     "8.8.8.8:53",
			RecordType: "A",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for invalid domain")
	}
}

func TestDNSCheckerUnsupportedType(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-unsupported-type",
		Type:    config.CheckTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "google.com",
			Server:     "8.8.8.8:53",
			RecordType: "INVALID",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for unsupported record type")
	}
	if !strings.Contains(result.Error, "unsupported record type") {
		t.Errorf("expected 'unsupported record type' error, got: %s", result.Error)
	}
}

func TestDNSCheckerMissingConfig(t *testing.T) {
	prober := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name: "test-nil-dns",
		Type: config.CheckTypeDNS,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil DNS config")
	}
	if result.Error != "missing DNS check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
