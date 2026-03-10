package budget

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/probe"
)

func TestLoadBudgets(t *testing.T) {
	content := `
- probe: checkout_flow
  thresholds:
    lcp: 2500
    fcp: 1800
    ttfb: 800
  alert_on: exceed
- probe: "*"
  thresholds:
    lcp: 4000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "budgets.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	budgets, err := LoadBudgets(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(budgets) != 2 {
		t.Fatalf("expected 2 budgets, got %d", len(budgets))
	}
	if budgets[0].Probe != "checkout_flow" {
		t.Errorf("expected probe=checkout_flow, got %s", budgets[0].Probe)
	}
	if budgets[0].Thresholds["lcp"] != 2500 {
		t.Errorf("expected lcp=2500, got %f", budgets[0].Thresholds["lcp"])
	}
	if budgets[1].Probe != "*" {
		t.Errorf("expected probe=*, got %s", budgets[1].Probe)
	}
	if budgets[1].AlertOn != "exceed" {
		t.Errorf("expected default alert_on=exceed, got %s", budgets[1].AlertOn)
	}
}

func TestEvaluateNoViolations(t *testing.T) {
	result := &probe.Result{
		Name:    "test",
		Type:    config.ProbeTypeHTTP,
		Success: true,
		Duration: 500 * time.Millisecond,
	}

	budgets := []Budget{
		{
			Probe:      "*",
			Thresholds: map[string]float64{"duration": 5000},
		},
	}

	violations := Evaluate(result, budgets)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(violations))
	}
}

func TestEvaluateWithViolations(t *testing.T) {
	result := &probe.Result{
		Name:     "slow-page",
		Type:     config.ProbeTypePlaywright,
		Success:  true,
		Duration: 3 * time.Second,
		WebVitals: &probe.WebVitals{
			LCP:  3500,
			FCP:  2000,
			TTFB: 900,
		},
	}

	budgets := []Budget{
		{
			Probe: "slow-page",
			Thresholds: map[string]float64{
				"lcp":  2500,
				"fcp":  1800,
				"ttfb": 1000,
			},
		},
	}

	violations := Evaluate(result, budgets)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations (lcp, fcp), got %d: %v", len(violations), violations)
	}
}

func TestEvaluateWildcard(t *testing.T) {
	result := &probe.Result{
		Name:     "any-probe",
		Type:     config.ProbeTypeHTTP,
		Success:  true,
		Duration: 10 * time.Second,
	}

	budgets := []Budget{
		{
			Probe:      "*",
			Thresholds: map[string]float64{"duration": 5000},
		},
	}

	violations := Evaluate(result, budgets)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Metric != "duration" {
		t.Errorf("expected metric=duration, got %s", violations[0].Metric)
	}
}

func TestEvaluateNamedProbeOnly(t *testing.T) {
	result := &probe.Result{
		Name:     "other-probe",
		Type:     config.ProbeTypeHTTP,
		Success:  true,
		Duration: 10 * time.Second,
	}

	budgets := []Budget{
		{
			Probe:      "specific-probe",
			Thresholds: map[string]float64{"duration": 5000},
		},
	}

	violations := Evaluate(result, budgets)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for non-matching probe, got %d", len(violations))
	}
}
