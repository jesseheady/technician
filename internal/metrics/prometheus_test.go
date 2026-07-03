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
