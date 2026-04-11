package budget

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/check"
)

func TestLoadBudgets(t *testing.T) {
	content := `
- check: checkout_flow
  thresholds:
    lcp: 2500
    fcp: 1800
    ttfb: 800
  alert_on: exceed
- check: "*"
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
	if budgets[0].Check != "checkout_flow" {
		t.Errorf("expected check=checkout_flow, got %s", budgets[0].Check)
	}
	if budgets[0].Thresholds["lcp"] != 2500 {
		t.Errorf("expected lcp=2500, got %f", budgets[0].Thresholds["lcp"])
	}
	if budgets[1].Check != "*" {
		t.Errorf("expected check=*, got %s", budgets[1].Check)
	}
	if budgets[1].AlertOn != "exceed" {
		t.Errorf("expected default alert_on=exceed, got %s", budgets[1].AlertOn)
	}
}

func TestEvaluateNoViolations(t *testing.T) {
	result := &check.Result{
		Name:     "test",
		Type:     config.CheckTypeHTTP,
		Success:  true,
		Duration: 500 * time.Millisecond,
	}

	budgets := []Budget{
		{
			Check:      "*",
			Thresholds: map[string]float64{"duration": 5000},
		},
	}

	violations := Evaluate(result, budgets)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(violations))
	}
}

func TestEvaluateWithViolations(t *testing.T) {
	result := &check.Result{
		Name:     "slow-page",
		Type:     config.CheckTypePlaywright,
		Success:  true,
		Duration: 3 * time.Second,
		WebVitals: &check.WebVitals{
			LCP:  3500,
			FCP:  2000,
			TTFB: 900,
		},
	}

	budgets := []Budget{
		{
			Check: "slow-page",
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
	result := &check.Result{
		Name:     "any-check",
		Type:     config.CheckTypeHTTP,
		Success:  true,
		Duration: 10 * time.Second,
	}

	budgets := []Budget{
		{
			Check:      "*",
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

func TestEvaluateNamedCheckOnly(t *testing.T) {
	result := &check.Result{
		Name:     "other-check",
		Type:     config.CheckTypeHTTP,
		Success:  true,
		Duration: 10 * time.Second,
	}

	budgets := []Budget{
		{
			Check:      "specific-check",
			Thresholds: map[string]float64{"duration": 5000},
		},
	}

	violations := Evaluate(result, budgets)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for non-matching check, got %d", len(violations))
	}
}
