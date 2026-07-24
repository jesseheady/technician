package budget

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var sampleViolations = []Violation{
	{Check: "home", Metric: "lcp", Threshold: 2500, Actual: 3100},
	{Check: "api", Metric: "ttfb", Threshold: 200, Actual: 450},
}

func TestTextReporter(t *testing.T) {
	var buf bytes.Buffer
	if err := NewTextReporter(&buf).Report(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "All performance budgets passed") {
		t.Errorf("empty report = %q, want pass message", buf.String())
	}

	buf.Reset()
	if err := NewTextReporter(&buf).Report(sampleViolations); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "2 budget violation(s)") || !strings.Contains(out, "home") {
		t.Errorf("report missing count or check name: %q", out)
	}
}

func TestJSONReporterIsValidAndFlagsPass(t *testing.T) {
	var buf bytes.Buffer
	if err := NewJSONReporter(&buf).Report(sampleViolations); err != nil {
		t.Fatal(err)
	}
	var got jsonReport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.Pass {
		t.Error("Pass = true with violations present, want false")
	}
	if len(got.Violations) != 2 || got.Violations[0].Status != "fail" {
		t.Errorf("violations = %+v, want 2 with status fail", got.Violations)
	}

	buf.Reset()
	_ = NewJSONReporter(&buf).Report(nil)
	var empty jsonReport
	_ = json.Unmarshal(buf.Bytes(), &empty)
	if !empty.Pass {
		t.Error("Pass = false with no violations, want true")
	}
}

func TestGHAReporterEmitsAnnotations(t *testing.T) {
	var buf bytes.Buffer
	r := &GHAReporter{w: &buf}
	if err := r.Report(sampleViolations); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "::warning title=Budget Violation::") {
		t.Errorf("missing per-violation warning annotation: %q", out)
	}
	if !strings.Contains(out, "::error title=Budget Check Failed::2 budget") {
		t.Errorf("missing summary error annotation: %q", out)
	}
}

func TestNewReporterSelectsByFormat(t *testing.T) {
	var buf bytes.Buffer
	cases := map[string]any{
		"json":    &JSONReporter{},
		"gha":     &GHAReporter{},
		"github":  &GHAReporter{},
		"text":    &TextReporter{},
		"":        &TextReporter{},
		"unknown": &TextReporter{},
	}
	for format, want := range cases {
		got := NewReporter(format, &buf)
		if gotType, wantType := typeName(got), typeName(want); gotType != wantType {
			t.Errorf("NewReporter(%q) = %s, want %s", format, gotType, wantType)
		}
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *JSONReporter:
		return "JSON"
	case *GHAReporter:
		return "GHA"
	case *TextReporter:
		return "Text"
	default:
		return "?"
	}
}
