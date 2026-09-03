package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

// gaugeValue reads the current value of a plain Gauge without pulling in extra
// test helpers.
func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("reading gauge: %v", err)
	}
	return m.GetGauge().GetValue()
}

// recordedCheckNames returns the set of check names currently present as series
// on the checkUp gauge — i.e. the names the cardinality guard let through.
func recordedCheckNames(t *testing.T) map[string]bool {
	t.Helper()
	ch := make(chan prometheus.Metric, 1024)
	checkUp.Collect(ch)
	close(ch)

	names := make(map[string]bool)
	for m := range ch {
		var d dto.Metric
		if err := m.Write(&d); err != nil {
			t.Fatalf("reading metric: %v", err)
		}
		for _, l := range d.GetLabel() {
			if l.GetName() == "name" {
				names[l.GetValue()] = true
			}
		}
	}
	return names
}

// resetCardinalityGuard isolates a test from guard state left by other tests,
// restoring the package globals afterwards.
func resetCardinalityGuard(t *testing.T) {
	t.Helper()
	cardinalityMu.Lock()
	prevMax, prevSeen, prevLogged := maxCheckCardinality, seenCheckNames, cardinalityLimit
	maxCheckCardinality = config.DefaultMaxCheckCardinality
	seenCheckNames = make(map[string]struct{})
	cardinalityLimit = false
	cardinalityMu.Unlock()

	t.Cleanup(func() {
		cardinalityMu.Lock()
		defer cardinalityMu.Unlock()
		maxCheckCardinality, seenCheckNames, cardinalityLimit = prevMax, prevSeen, prevLogged
	})
}

// TestCardinalityGuardHonorsConfiguredLimit verifies the guard enforces the
// configured limit rather than the compile-time default (issue #228).
func TestCardinalityGuardHonorsConfiguredLimit(t *testing.T) {
	resetCardinalityGuard(t)
	SetMaxCheckCardinality(2)

	for _, name := range []string{"card-a", "card-b", "card-over"} {
		r := check.NewResult(name, config.CheckTypeHTTP, nil)
		r.Success = true
		RecordResult(r)
	}
	t.Cleanup(func() {
		for _, name := range []string{"card-a", "card-b", "card-over"} {
			checkUp.DeletePartialMatch(prometheus.Labels{"name": name})
		}
	})

	names := recordedCheckNames(t)
	if !names["card-a"] || !names["card-b"] {
		t.Fatalf("expected the first 2 names under the limit to be recorded, got %v", names)
	}
	if names["card-over"] {
		t.Errorf("expected 'card-over' to be dropped at limit=2, but it was recorded")
	}
}

// TestSetMaxCheckCardinalityIgnoresNonPositive guards the config path: an unset
// limit must not disable the guard by setting it to zero.
func TestSetMaxCheckCardinalityIgnoresNonPositive(t *testing.T) {
	resetCardinalityGuard(t)
	SetMaxCheckCardinality(750)
	SetMaxCheckCardinality(0)

	cardinalityMu.Lock()
	got := maxCheckCardinality
	cardinalityMu.Unlock()

	if got != 750 {
		t.Errorf("expected limit to stay 750 after a 0 override, got %d", got)
	}
}

// TestLastRunTimestampFreshness verifies the data-freshness gauge advances on a
// recorded result but not on an infra error — the property the staleness grace
// period (issue #111) relies on to detect data gaps.
func TestLastRunTimestampFreshness(t *testing.T) {
	// A recorded (non-infra) result marks data fresh.
	ok := check.NewResult("freshness-probe", config.CheckTypeHTTP, nil)
	ok.Success = true
	before := time.Now().Unix()
	RecordResult(ok)
	if got := int64(gaugeValue(t, lastRunTimestamp)); got < before {
		t.Fatalf("expected lastRunTimestamp >= %d after a recorded result, got %d", before, got)
	}

	// Rewind the gauge to simulate a stale worker, then confirm an infra error
	// does NOT advance it — a stretch of connectivity failures must keep the
	// signal frozen so the gap is detectable.
	stale := float64(time.Now().Add(-time.Hour).Unix())
	lastRunTimestamp.Set(stale)

	infra := check.NewResult("freshness-probe", config.CheckTypeHTTP, nil)
	infra.InfraError = true
	RecordResult(infra)
	if got := gaugeValue(t, lastRunTimestamp); got != stale {
		t.Errorf("infra error advanced freshness gauge: want %v (unchanged), got %v", stale, got)
	}

	// A subsequent recorded result advances it again past the stale value.
	RecordResult(ok)
	if got := gaugeValue(t, lastRunTimestamp); got <= stale {
		t.Errorf("recorded result did not advance freshness gauge past stale value %v, got %v", stale, got)
	}
}

// inpSeriesFor reports whether browserINP holds a series for the given check
// name, and its value.
func inpSeriesFor(t *testing.T, name string) (float64, bool) {
	t.Helper()
	ch := make(chan prometheus.Metric, 1024)
	browserINP.Collect(ch)
	close(ch)

	for m := range ch {
		var d dto.Metric
		if err := m.Write(&d); err != nil {
			t.Fatalf("reading metric: %v", err)
		}
		for _, l := range d.GetLabel() {
			if l.GetName() == "name" && l.GetValue() == name {
				return d.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

// A check whose script makes no interaction has no INP. Record no series for
// it, because a 0 there is indistinguishable from a very fast interaction.
func TestBrowserINPSkippedWithoutInteraction(t *testing.T) {
	result := &check.Result{
		Name:      "load-only-flow",
		Type:      config.CheckTypePlaywright,
		Success:   true,
		WebVitals: &check.WebVitals{LCP: 1200, CLS: 0.02, INP: 0},
	}
	recordBrowserMetrics(result, siteLabels(result))

	if v, ok := inpSeriesFor(t, result.Name); ok {
		t.Errorf("expected no INP series without an interaction, got %v", v)
	}
}

func TestBrowserINPRecordedWithInteraction(t *testing.T) {
	result := &check.Result{
		Name:      "interactive-flow",
		Type:      config.CheckTypePlaywright,
		Success:   true,
		WebVitals: &check.WebVitals{LCP: 1200, CLS: 0.02, INP: 250},
	}
	recordBrowserMetrics(result, siteLabels(result))

	v, ok := inpSeriesFor(t, result.Name)
	if !ok {
		t.Fatal("expected an INP series after an interaction")
	}
	if v != 250 {
		t.Errorf("expected INP 250, got %v", v)
	}
}
