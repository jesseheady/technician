package budget

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Reporter interface {
	Report(violations []Violation) error
}

type TextReporter struct {
	w io.Writer
}

func NewTextReporter(w io.Writer) *TextReporter {
	return &TextReporter{w: w}
}

func (r *TextReporter) Report(violations []Violation) error {
	if len(violations) == 0 {
		fmt.Fprintln(r.w, "All performance budgets passed.")
		return nil
	}

	fmt.Fprintf(r.w, "%d budget violation(s) found:\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(r.w, "  FAIL: %s\n", v)
	}
	return nil
}

type JSONReporter struct {
	w io.Writer
}

func NewJSONReporter(w io.Writer) *JSONReporter {
	return &JSONReporter{w: w}
}

type jsonViolation struct {
	Check     string  `json:"check"`
	Metric    string  `json:"metric"`
	Threshold float64 `json:"threshold"`
	Actual    float64 `json:"actual"`
	Status    string  `json:"status"`
}

type jsonReport struct {
	Pass       bool            `json:"pass"`
	Violations []jsonViolation `json:"violations"`
}

func (r *JSONReporter) Report(violations []Violation) error {
	report := jsonReport{
		Pass:       len(violations) == 0,
		Violations: make([]jsonViolation, len(violations)),
	}
	for i, v := range violations {
		report.Violations[i] = jsonViolation{
			Check:     v.Check,
			Metric:    v.Metric,
			Threshold: v.Threshold,
			Actual:    v.Actual,
			Status:    "fail",
		}
	}

	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

type GHAReporter struct {
	w io.Writer
}

func NewGHAReporter() *GHAReporter {
	return &GHAReporter{w: os.Stdout}
}

func (r *GHAReporter) Report(violations []Violation) error {
	for _, v := range violations {
		// GitHub Actions annotation format
		fmt.Fprintf(r.w, "::warning title=Budget Violation::%s\n", v)
	}
	if len(violations) > 0 {
		fmt.Fprintf(r.w, "::error title=Budget Check Failed::%d budget violation(s) found\n", len(violations))
	}
	return nil
}

func NewReporter(format string, w io.Writer) Reporter {
	switch strings.ToLower(format) {
	case "json":
		return NewJSONReporter(w)
	case "gha", "github":
		return NewGHAReporter()
	default:
		return NewTextReporter(w)
	}
}
