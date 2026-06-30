package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

// TestSchedulerRunsChecksOnStartup verifies that Start executes each check
// once immediately, without waiting for the first cron tick. The schedule is
// set far in the future so any result observed must come from the startup run.
func TestSchedulerRunsChecksOnStartup(t *testing.T) {
	m := &mockChecker{results: []*check.Result{{Success: true}}}
	reg := NewCheckerRegistry()
	reg.Register(m)

	// Empty config -> ResolveOrigin returns nil -> ComputeStagger returns 0,
	// so the startup run fires without delay.
	cfg := &config.Config{}
	checks := []config.CheckConfig{
		{Name: "startup-check", Type: config.CheckTypeHTTP, Schedule: "0 0 0 1 1 *"},
	}
	s := New(cfg, checks, reg, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case res := <-s.Results():
		if res == nil || !res.Success {
			t.Fatalf("expected a successful startup result, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a check result from the startup run, got none")
	}
}

// TestSchedulerStartupSkipsUnregisteredType ensures startup does not panic or
// emit results for a check whose type has no registered checker.
func TestSchedulerStartupSkipsUnregisteredType(t *testing.T) {
	reg := NewCheckerRegistry() // no checkers registered
	cfg := &config.Config{}
	checks := []config.CheckConfig{
		{Name: "orphan", Type: config.CheckTypeHTTP, Schedule: "0 0 0 1 1 *"},
	}
	s := New(cfg, checks, reg, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case res := <-s.Results():
		t.Fatalf("expected no result for unregistered type, got %+v", res)
	case <-time.After(300 * time.Millisecond):
		// no result, as expected
	}
}
