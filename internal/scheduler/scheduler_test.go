package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

// countingChecker is a race-safe checker for tests where the startup run and a
// scheduled tick may invoke Run concurrently.
type countingChecker struct{ runs atomic.Int64 }

func (c *countingChecker) Type() config.CheckType { return config.CheckTypeHTTP }

func (c *countingChecker) Run(_ context.Context, _ *config.CheckConfig, _ *config.Origin) *check.Result {
	c.runs.Add(1)
	return &check.Result{Success: true}
}

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

// TestSchedulerRunsOnSchedule verifies the gronx-driven loop fires on the cron
// schedule: with an every-second schedule we expect the immediate startup run
// plus at least one scheduled tick within a few seconds.
func TestSchedulerRunsOnSchedule(t *testing.T) {
	c := &countingChecker{}
	reg := NewCheckerRegistry()
	reg.Register(c)

	cfg := &config.Config{}
	checks := []config.CheckConfig{
		{Name: "tick", Type: config.CheckTypeHTTP, Schedule: "* * * * * *"}, // every second
	}
	s := New(cfg, checks, reg, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	got := 0
	deadline := time.After(4 * time.Second)
	for got < 2 {
		select {
		case <-s.Results():
			got++
		case <-deadline:
			t.Fatalf("expected >=2 results (startup + scheduled tick), got %d", got)
		}
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
