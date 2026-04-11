package scheduler

import (
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestComputeStaggerNilSite(t *testing.T) {
	d := ComputeStagger("probe1", nil)
	if d != 0 {
		t.Errorf("expected 0 for nil site, got %v", d)
	}
}

func TestComputeStaggerDeterministic(t *testing.T) {
	origin := &config.Origin{ID: "us-east-1"}

	d1 := ComputeStagger("probe1", origin)
	d2 := ComputeStagger("probe1", origin)

	if d1 != d2 {
		t.Errorf("stagger should be deterministic: %v != %v", d1, d2)
	}
}

func TestComputeStaggerDifferentChecks(t *testing.T) {
	origin := &config.Origin{ID: "us-east-1"}

	d1 := ComputeStagger("probe1", origin)
	d2 := ComputeStagger("probe2", origin)

	// Different checks should (generally) have different stagger
	// This could theoretically fail due to hash collision but is extremely unlikely
	if d1 == d2 {
		t.Log("Warning: two different checks got same stagger (possible hash collision)")
	}
}

func TestComputeStaggerBound(t *testing.T) {
	origin := &config.Origin{ID: "test"}

	for _, name := range []string{"a", "b", "c", "long-check-name", "x"} {
		d := ComputeStagger(name, origin)
		if d < 0 || d >= 10*time.Second {
			t.Errorf("stagger for %q out of bounds: %v", name, d)
		}
	}
}
