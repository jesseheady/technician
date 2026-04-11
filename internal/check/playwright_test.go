package check

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

func TestPlaywrightCheckerDefaultMaxBrowsers(t *testing.T) {
	p := NewPlaywrightChecker("/nonexistent/run.js", 0)
	if p.maxBrowsers != 2 {
		t.Errorf("expected default max_browsers=2, got %d", p.maxBrowsers)
	}
	if cap(p.browserSem) != 2 {
		t.Errorf("expected semaphore capacity=2, got %d", cap(p.browserSem))
	}
}

func TestPlaywrightCheckerCustomMaxBrowsers(t *testing.T) {
	p := NewPlaywrightChecker("/nonexistent/run.js", 5)
	if p.maxBrowsers != 5 {
		t.Errorf("expected max_browsers=5, got %d", p.maxBrowsers)
	}
	if cap(p.browserSem) != 5 {
		t.Errorf("expected semaphore capacity=5, got %d", cap(p.browserSem))
	}
}

func TestPlaywrightCheckerNilConfig(t *testing.T) {
	p := NewPlaywrightChecker("/nonexistent/run.js", 2)
	result := p.Run(context.Background(), &config.CheckConfig{
		Name: "test-nil-config",
	}, nil)

	if result.Success {
		t.Error("expected failure for nil Playwright config")
	}
	if !result.InfraError {
		t.Error("expected InfraError for nil Playwright config")
	}
}

func TestPlaywrightCheckerSemaphoreBlocksOnFull(t *testing.T) {
	// Create a checker with max 1 browser
	p := NewPlaywrightChecker("/nonexistent/run.js", 1)

	// Manually occupy the semaphore slot
	p.browserSem <- struct{}{}

	// Try to run with a short timeout — should fail waiting for slot
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := p.Run(ctx, &config.CheckConfig{
		Name:       "test-blocked",
		Timeout:    100 * time.Millisecond,
		Playwright: &config.PlaywrightCheckConfig{Script: "test.js"},
	}, nil)

	if result.Success {
		t.Error("expected failure when semaphore is full")
	}
	if !result.InfraError {
		t.Error("expected InfraError when blocked on semaphore")
	}

	// Release the slot
	<-p.browserSem
}

func TestPlaywrightCheckerSemaphoreLimitsConcurrency(t *testing.T) {
	maxBrowsers := 2
	p := NewPlaywrightChecker("/nonexistent/run.js", maxBrowsers)

	var (
		maxConcurrent atomic.Int32
		current       atomic.Int32
		wg            sync.WaitGroup
	)

	// Launch more goroutines than max_browsers, each acquiring a slot briefly
	total := 6
	wg.Add(total)

	for i := 0; i < total; i++ {
		go func() {
			defer wg.Done()

			// Acquire slot
			p.browserSem <- struct{}{}

			cur := current.Add(1)
			// Track max concurrent
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}

			time.Sleep(10 * time.Millisecond) // simulate work
			current.Add(-1)

			<-p.browserSem
		}()
	}

	wg.Wait()

	if got := maxConcurrent.Load(); got > int32(maxBrowsers) {
		t.Errorf("max concurrent browsers exceeded limit: got %d, want <= %d", got, maxBrowsers)
	}
}
