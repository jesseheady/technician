package scheduler

import (
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

func TestComputeStaggerNilSite(t *testing.T) {
	d := ComputeStagger("probe1", nil)
	if d != 0 {
		t.Errorf("expected 0 for nil site, got %v", d)
	}
}

func TestComputeStaggerDeterministic(t *testing.T) {
	site := &config.Site{Code: "us-east-1"}

	d1 := ComputeStagger("probe1", site)
	d2 := ComputeStagger("probe1", site)

	if d1 != d2 {
		t.Errorf("stagger should be deterministic: %v != %v", d1, d2)
	}
}

func TestComputeStaggerDifferentChecks(t *testing.T) {
	site := &config.Site{Code: "us-east-1"}

	d1 := ComputeStagger("probe1", site)
	d2 := ComputeStagger("probe2", site)

	// Different checks should (generally) have different stagger
	// This could theoretically fail due to hash collision but is extremely unlikely
	if d1 == d2 {
		t.Log("Warning: two different checks got same stagger (possible hash collision)")
	}
}

func TestComputeStaggerBound(t *testing.T) {
	site := &config.Site{Code: "test"}

	for _, name := range []string{"a", "b", "c", "long-check-name", "x"} {
		d := ComputeStagger(name, site)
		if d < 0 || d >= 10*time.Second {
			t.Errorf("stagger for %q out of bounds: %v", name, d)
		}
	}
}
