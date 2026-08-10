package check

import (
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestResultThresholdDefaults(t *testing.T) {
	// Zero values fall back to the documented defaults.
	r := &Result{}
	if got := r.CertWarnDays(); got != 30 {
		t.Errorf("CertWarnDays default = %d, want 30", got)
	}
	if got := r.CertCriticalDays(); got != 7 {
		t.Errorf("CertCriticalDays default = %d, want 7", got)
	}
	if got := r.DomainWarnDays(); got != 30 {
		t.Errorf("DomainWarnDays default = %d, want 30", got)
	}
	if got := r.DomainCriticalDays(); got != 7 {
		t.Errorf("DomainCriticalDays default = %d, want 7", got)
	}
}

func TestResultThresholdOverrides(t *testing.T) {
	// Positive values are returned as-is.
	r := &Result{
		CertWarnDaysVal:   45,
		CertCritDaysVal:   10,
		DomainWarnDaysVal: 60,
		DomainCritDaysVal: 14,
	}
	if got := r.CertWarnDays(); got != 45 {
		t.Errorf("CertWarnDays = %d, want 45", got)
	}
	if got := r.CertCriticalDays(); got != 10 {
		t.Errorf("CertCriticalDays = %d, want 10", got)
	}
	if got := r.DomainWarnDays(); got != 60 {
		t.Errorf("DomainWarnDays = %d, want 60", got)
	}
	if got := r.DomainCriticalDays(); got != 14 {
		t.Errorf("DomainCriticalDays = %d, want 14", got)
	}
}

func TestDegradedLatencyUsesDurationByDefault(t *testing.T) {
	// Non-ICMP checks time a single operation, so Duration is the latency.
	r := &Result{Type: config.CheckTypeHTTP, Duration: 900 * time.Millisecond}
	if got := r.DegradedLatency(); got != 900*time.Millisecond {
		t.Errorf("DegradedLatency = %v, want 900ms", got)
	}
}

func TestDegradedLatencyICMPUsesAvgRTT(t *testing.T) {
	// Duration sums every probe; the threshold applies to one probe, so the
	// result must not scale with count.
	r := &Result{
		Type:            config.CheckTypeICMP,
		Duration:        250 * time.Millisecond, // 5 probes at ~50ms
		ICMPPacketsRecv: 5,
		ICMPAvgRTT:      50 * time.Millisecond,
	}
	if got := r.DegradedLatency(); got != 50*time.Millisecond {
		t.Errorf("DegradedLatency = %v, want 50ms (per-probe, not the 250ms sum)", got)
	}
}

func TestDegradedLatencyICMPWithoutRepliesFallsBack(t *testing.T) {
	// No replies means ICMPAvgRTT was never computed; reporting 0 would read
	// as a perfectly fast check.
	r := &Result{
		Type:            config.CheckTypeICMP,
		Duration:        3 * time.Second,
		ICMPPacketsRecv: 0,
	}
	if got := r.DegradedLatency(); got != 3*time.Second {
		t.Errorf("DegradedLatency = %v, want 3s", got)
	}
}
