package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

type mockChecker struct {
	results []*check.Result
	calls   int
}

func (m *mockChecker) Type() config.CheckType { return config.CheckTypeHTTP }

func (m *mockChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *check.Result {
	idx := m.calls
	m.calls++
	if idx < len(m.results) {
		return m.results[idx]
	}
	return m.results[len(m.results)-1]
}

func TestRunWithRetryNoRetryOnSuccess(t *testing.T) {
	m := &mockChecker{
		results: []*check.Result{
			{Success: true},
		},
	}
	cfg := &config.CheckConfig{
		Name: "test-success",
		Retry: &config.RetryPolicy{
			Count: 2,
			Delay: 10 * time.Millisecond,
		},
	}

	result := runWithRetry(context.Background(), m, cfg, nil)

	if !result.Success {
		t.Error("expected success")
	}
	if m.calls != 1 {
		t.Errorf("expected 1 call, got %d", m.calls)
	}
}

func TestRunWithRetryNoConfig(t *testing.T) {
	m := &mockChecker{
		results: []*check.Result{
			{Success: false, Error: "connection refused"},
		},
	}
	cfg := &config.CheckConfig{
		Name:  "test-no-retry",
		Retry: nil,
	}

	result := runWithRetry(context.Background(), m, cfg, nil)

	if result.Success {
		t.Error("expected failure")
	}
	if m.calls != 1 {
		t.Errorf("expected 1 call, got %d", m.calls)
	}
}

func TestRunWithRetryEventualSuccess(t *testing.T) {
	m := &mockChecker{
		results: []*check.Result{
			{Success: false, Error: "connection refused"},
			{Success: true},
		},
	}
	cfg := &config.CheckConfig{
		Name: "test-eventual-success",
		Retry: &config.RetryPolicy{
			Count: 2,
			Delay: 10 * time.Millisecond,
		},
	}

	result := runWithRetry(context.Background(), m, cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if m.calls != 2 {
		t.Errorf("expected 2 calls, got %d", m.calls)
	}
}

func TestRunWithRetryAllFail(t *testing.T) {
	m := &mockChecker{
		results: []*check.Result{
			{Success: false, Error: "fail 1"},
			{Success: false, Error: "fail 2"},
			{Success: false, Error: "fail 3"},
		},
	}
	cfg := &config.CheckConfig{
		Name: "test-all-fail",
		Retry: &config.RetryPolicy{
			Count: 2,
			Delay: 10 * time.Millisecond,
		},
	}

	result := runWithRetry(context.Background(), m, cfg, nil)

	if result.Success {
		t.Error("expected failure")
	}
	if m.calls != 3 {
		t.Errorf("expected 3 calls (1 original + 2 retries), got %d", m.calls)
	}
}

func TestRunWithRetryExponentialBackoff(t *testing.T) {
	m := &mockChecker{
		results: []*check.Result{
			{Success: false, Error: "fail 1"},
			{Success: false, Error: "fail 2"},
			{Success: true},
		},
	}
	cfg := &config.CheckConfig{
		Name: "test-exponential",
		Retry: &config.RetryPolicy{
			Count:   3,
			Delay:   10 * time.Millisecond,
			Backoff: "exponential",
		},
	}

	start := time.Now()
	result := runWithRetry(context.Background(), m, cfg, nil)
	elapsed := time.Since(start)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if m.calls != 3 {
		t.Errorf("expected 3 calls, got %d", m.calls)
	}
	// First retry waits 10ms, second retry waits 20ms (exponential doubling).
	// Total delay should be at least 30ms.
	if elapsed < 30*time.Millisecond {
		t.Errorf("expected at least 30ms for exponential backoff, got %v", elapsed)
	}
}
