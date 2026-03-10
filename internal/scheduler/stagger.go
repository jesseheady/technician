package scheduler

import (
	"hash/fnv"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
)

// ComputeStagger returns a deterministic stagger delay based on probe name and site.
// This ensures probes from different sites don't all fire at exactly the same time,
// while remaining stable across restarts.
func ComputeStagger(probeName string, site *config.Site) time.Duration {
	if site == nil {
		return 0
	}

	h := fnv.New32a()
	h.Write([]byte(probeName))
	h.Write([]byte(site.Code))
	hash := h.Sum32()

	// Stagger up to 10 seconds
	staggerMs := hash % 10000
	return time.Duration(staggerMs) * time.Millisecond
}
