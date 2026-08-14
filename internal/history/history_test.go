package history

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

func result(name string, success bool, ts time.Time) *check.Result {
	return &check.Result{
		Name:      name,
		Type:      config.CheckTypeHTTP,
		Success:   success,
		Timestamp: ts,
		Duration:  100 * time.Millisecond,
	}
}

// openTemp returns a Store on a fresh temp DB; the caller records, then Close
// flushes the async writer so a reopened Store can read deterministically.
func openTemp(t *testing.T, retention time.Duration) (string, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "results.db")
	s, err := New(path, retention)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return path, s
}

func TestRecordAndQueryUptime(t *testing.T) {
	now := time.Now()
	path, s := openTemp(t, time.Hour)
	for _, ok := range []bool{true, true, true, false} { // 3/4 up
		s.Record(result("check-a", ok, now))
	}
	if err := s.Close(); err != nil { // flushes buffered inserts
		t.Fatalf("Close: %v", err)
	}

	s2, err := New(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	ratio, ok, err := s2.UptimeOverWindow("check-a", time.Hour)
	if err != nil {
		t.Fatalf("UptimeOverWindow: %v", err)
	}
	if !ok {
		t.Fatal("expected results in window")
	}
	if ratio != 0.75 {
		t.Errorf("uptime = %v, want 0.75", ratio)
	}
}

func TestInfraErrorsAreNotRecorded(t *testing.T) {
	now := time.Now()
	path, s := openTemp(t, time.Hour)
	infra := result("check-b", false, now)
	infra.InfraError = true
	s.Record(infra)
	s.Record(nil) // must be ignored, not panic
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := New(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	if _, ok, err := s2.UptimeOverWindow("check-b", time.Hour); err != nil || ok {
		t.Errorf("expected no recorded results for an infra error; ok=%v err=%v", ok, err)
	}
}

func TestRetentionPruneDropsOldRows(t *testing.T) {
	now := time.Now()
	path, s := openTemp(t, time.Hour)
	s.Record(result("check-c", false, now.Add(-2*time.Hour))) // stale (older than retention)
	s.Record(result("check-c", true, now))                    // fresh
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := New(path, time.Hour) // retention 1h
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	s2.pruneOld()

	ratio, ok, err := s2.UptimeOverWindow("check-c", 24*time.Hour)
	if err != nil {
		t.Fatalf("UptimeOverWindow: %v", err)
	}
	if !ok || ratio != 1.0 {
		t.Errorf("after prune, uptime = %v (ok=%v), want 1.0 with the stale failure gone", ratio, ok)
	}
}
