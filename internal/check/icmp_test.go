package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestPayloadMatches(t *testing.T) {
	want := []byte("technician-check")
	if !payloadMatches(want, want) {
		t.Error("identical payloads should match")
	}
	if !payloadMatches(append([]byte("technician-check"), 0, 0), want) {
		t.Error("padded payload should match on prefix")
	}
	if payloadMatches([]byte("nope"), want) {
		t.Error("differing payload should not match")
	}
}

func TestICMPCheckerLoopback(t *testing.T) {
	checker := NewICMPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-icmp-loopback",
		Type:    config.CheckTypeICMP,
		Timeout: 3 * time.Second,
		ICMP:    &config.ICMPCheckConfig{Host: "127.0.0.1", Count: 2},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(context.Background(), cfg, origin)

	// Opening an ICMP socket needs privileges the environment may not grant
	// (CI runners, restricted containers). Skip rather than fail there.
	if strings.Contains(result.Error, "listen ICMP") || strings.Contains(result.Error, "permitted") {
		t.Skipf("ICMP not permitted in this environment: %s", result.Error)
	}

	// The socket opened, so the send loop ran to completion regardless of
	// whether replies came back.
	if result.ICMPPacketsSent != 2 {
		t.Errorf("expected 2 packets sent, got %d", result.ICMPPacketsSent)
	}

	// Reply delivery on loopback is platform-dependent (unprivileged ICMP on
	// macOS, firewalls, etc.); the container sweep asserts real replies on
	// Linux. Here, only check aggregation when replies actually arrived.
	if !result.Success {
		t.Skipf("no ICMP replies in this environment (sent=%d recv=%d)", result.ICMPPacketsSent, result.ICMPPacketsRecv)
	}
	if result.ICMPAvgRTT == 0 {
		t.Error("expected non-zero average RTT when replies received")
	}
}

func TestICMPCheckerMissingConfig(t *testing.T) {
	checker := NewICMPChecker()
	cfg := &config.CheckConfig{Name: "test-icmp-nil", Type: config.CheckTypeICMP}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil ICMP config")
	}
	if result.Error != "missing ICMP check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
