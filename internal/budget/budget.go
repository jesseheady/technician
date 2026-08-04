package budget

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/jesseheady/technician/internal/check"
	"go.yaml.in/yaml/v3"
)

type Budget struct {
	Check      string             `yaml:"check"`
	Thresholds map[string]float64 `yaml:"thresholds"`
	AlertOn    string             `yaml:"alert_on"`
}

type Violation struct {
	Check     string
	Metric    string
	Threshold float64
	Actual    float64
	Inherited bool // threshold came from the "*" entry, not this check's own
}

func (v Violation) String() string {
	s := fmt.Sprintf("%s: %s = %.2f (threshold: %.2f", v.Check, v.Metric, v.Actual, v.Threshold)
	if v.Inherited {
		s += ", inherited from \"*\""
	}
	return s + ")"
}

func LoadBudgets(path string) ([]Budget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading budgets file: %w", err)
	}

	var budgets []Budget
	if err := yaml.Unmarshal(data, &budgets); err != nil {
		return nil, fmt.Errorf("parsing budgets: %w", err)
	}

	for i := range budgets {
		if budgets[i].AlertOn == "" {
			budgets[i].AlertOn = "exceed"
		}
	}

	return budgets, nil
}

func LoadBudgetsFromDir(configPath string) ([]Budget, error) {
	dir := filepath.Dir(configPath)

	// Try budgets.yml in same directory as config
	budgetPath := filepath.Join(dir, "budgets.yml")
	return LoadBudgets(budgetPath)
}

// CheckResult represents a single budget metric check (pass or fail).
type CheckResult struct {
	Check     string
	Metric    string
	Threshold float64
	Actual    float64
	Inherited bool // threshold came from the "*" entry, not this check's own
	Violated  bool
}

func Evaluate(result *check.Result, budgets []Budget) []Violation {
	var violations []Violation
	for _, c := range EvaluateAll(result, budgets) {
		if c.Violated {
			violations = append(violations, Violation{
				Check:     c.Check,
				Metric:    c.Metric,
				Threshold: c.Threshold,
				Actual:    c.Actual,
				Inherited: c.Inherited,
			})
		}
	}
	return violations
}

// EvaluateAll returns a CheckResult for every applicable budget metric,
// including passing checks (Violated=false).
//
// Thresholds merge per metric: the "*" entry supplies defaults and a named
// entry overrides them one metric at a time. A check that needs a looser
// duration keeps the rest of the defaults instead of dropping them, and
// CheckResult.Inherited records which thresholds came from "*".
func EvaluateAll(result *check.Result, budgets []Budget) []CheckResult {
	actuals := extractMetrics(result)

	thresholds := map[string]float64{}
	inherited := map[string]bool{}
	for _, budget := range budgets {
		if budget.Check != "*" {
			continue
		}
		for metric, threshold := range budget.Thresholds {
			thresholds[metric] = threshold
			inherited[metric] = true
		}
	}
	for _, budget := range budgets {
		if budget.Check != result.Name {
			continue
		}
		for metric, threshold := range budget.Thresholds {
			thresholds[metric] = threshold
			inherited[metric] = false
		}
	}

	var checks []CheckResult
	for _, metric := range slices.Sorted(maps.Keys(thresholds)) {
		actual, ok := actuals[metric]
		if !ok {
			continue
		}
		checks = append(checks, CheckResult{
			Check:     result.Name,
			Metric:    metric,
			Threshold: thresholds[metric],
			Actual:    actual,
			Inherited: inherited[metric],
			Violated:  actual > thresholds[metric],
		})
	}

	return checks
}

func extractMetrics(result *check.Result) map[string]float64 {
	m := map[string]float64{
		"duration": float64(result.Duration.Milliseconds()),
	}

	if result.Type == "http" {
		m["ttfb"] = float64(result.TTFBDuration.Milliseconds())
		m["dns"] = float64(result.DNSDuration.Milliseconds())
		m["tls"] = float64(result.TLSDuration.Milliseconds())
		m["response_bytes"] = float64(result.ResponseBytes)
	}

	if result.WebVitals != nil {
		v := result.WebVitals
		m["ttfb"] = v.TTFB
		m["fcp"] = v.FCP
		m["lcp"] = v.LCP
		m["cls"] = v.CLS
		m["inp"] = v.INP
		m["dom_complete"] = v.DOMComplete
	}

	if result.HARData != nil {
		m["total_transfer"] = float64(result.HARData.TotalTransferBytes)
	}

	m["resource_count"] = float64(result.ResourceCount)

	return m
}
