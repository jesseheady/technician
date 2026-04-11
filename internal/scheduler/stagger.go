package scheduler

import (
	"hash/fnv"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

// ComputeStagger returns a deterministic stagger delay based on check name and site.
// This ensures checks from different sites don't all fire at exactly the same time,
// while remaining stable across restarts.
func ComputeStagger(checkName string, site *config.Site) time.Duration {
	if site == nil {
		return 0
	}

	h := fnv.New32a()
	h.Write([]byte(checkName))
	h.Write([]byte(site.Code))
	hash := h.Sum32()

	// Stagger up to 10 seconds
	staggerMs := hash % 10000
	return time.Duration(staggerMs) * time.Millisecond
}
