package scheduler

import (
	"hash/fnv"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

// ComputeStagger returns a deterministic stagger delay based on check name and origin.
// This ensures checks from different sites don't all fire at exactly the same time,
// while remaining stable across restarts.
func ComputeStagger(checkName string, origin *config.Origin) time.Duration {
	if origin == nil {
		return 0
	}

	h := fnv.New32a()
	h.Write([]byte(checkName))
	h.Write([]byte(origin.ID))
	hash := h.Sum32()

	// Stagger up to 10 seconds
	staggerMs := hash % 10000
	return time.Duration(staggerMs) * time.Millisecond
}
