package check

import (
	"context"
	"testing"

	"github.com/jesseheady/technician/internal/config"
)

func TestParseMTROutputValid(t *testing.T) {
	data := []byte(`{
		"report": {
			"hubs": [
				{"count": 1, "host": "10.0.0.1", "ASN": 0, "Loss%": 0.0, "Avg": 1.2},
				{"count": 2, "host": "203.0.113.5", "ASN": 64500, "Loss%": 25.0, "Avg": 14.7}
			]
		}
	}`)

	hops, err := parseMTROutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hops) != 2 {
		t.Fatalf("expected 2 hops, got %d", len(hops))
	}
	if hops[0].Hop != 1 || hops[0].Host != "10.0.0.1" {
		t.Errorf("hop 0 mismatch: %+v", hops[0])
	}
	if hops[1].Hop != 2 || hops[1].ASN != 64500 || hops[1].LossPercent != 25.0 || hops[1].AvgMs != 14.7 {
		t.Errorf("hop 1 mismatch: %+v", hops[1])
	}
}

func TestParseMTROutputLeadingGarbage(t *testing.T) {
	// mtr-tiny in some builds prints progress text before the JSON object.
	data := []byte("Start: 2026-01-01\n{\"report\":{\"hubs\":[{\"host\":\"10.0.0.1\",\"Avg\":1.0}]}}")

	hops, err := parseMTROutput(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hops) != 1 || hops[0].Host != "10.0.0.1" {
		t.Errorf("expected the single hop to parse, got %+v", hops)
	}
}

func TestParseMTROutputNoJSON(t *testing.T) {
	data := []byte("mtr: Failure to open raw sockets: Operation not permitted")

	_, err := parseMTROutput(data)
	if err == nil {
		t.Fatal("expected error when output contains no JSON object")
	}
}

func TestParseMTROutputInvalidJSON(t *testing.T) {
	data := []byte(`{"report": {"hubs": [ this is not valid json`)

	_, err := parseMTROutput(data)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestTracerouteCheckerMissingConfig(t *testing.T) {
	checker := NewTracerouteChecker()
	cfg := &config.CheckConfig{Name: "test-tr-nil", Type: config.CheckTypeTraceroute}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil traceroute config")
	}
	if result.Error != "missing traceroute check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
