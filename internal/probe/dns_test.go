package probe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
)

func TestDNSProberARecord(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name:    "test-a-record",
		Type:    config.ProbeTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSProbeConfig{
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

func TestDNSProberAAAARecord(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name:    "test-aaaa-record",
		Type:    config.ProbeTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSProbeConfig{
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

func TestDNSProberTXTRecord(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name:    "test-txt-record",
		Type:    config.ProbeTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSProbeConfig{
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

func TestDNSProberExpectedMatch(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name:    "test-expected-match",
		Type:    config.ProbeTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSProbeConfig{
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

func TestDNSProberExpectedMismatch(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name:    "test-expected-mismatch",
		Type:    config.ProbeTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSProbeConfig{
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

func TestDNSProberInvalidDomain(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name:    "test-invalid-domain",
		Type:    config.ProbeTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSProbeConfig{
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

func TestDNSProberUnsupportedType(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name:    "test-unsupported-type",
		Type:    config.ProbeTypeDNS,
		Timeout: 10 * time.Second,
		DNS: &config.DNSProbeConfig{
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

func TestDNSProberMissingConfig(t *testing.T) {
	prober := NewDNSProber()
	cfg := &config.ProbeConfig{
		Name: "test-nil-dns",
		Type: config.ProbeTypeDNS,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil DNS config")
	}
	if result.Error != "missing DNS probe configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
