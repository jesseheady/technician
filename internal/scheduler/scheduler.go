package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/adhocore/gronx"
	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/metrics"
)

type ProberRegistry struct {
	mu       sync.RWMutex
	checkers map[config.CheckType]check.Checker
}

func NewCheckerRegistry() *ProberRegistry {
	return &ProberRegistry{
		checkers: make(map[config.CheckType]check.Checker),
	}
}

func (r *ProberRegistry) Register(p check.Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers[p.Type()] = p
}

func (r *ProberRegistry) RegisterMap(checkers map[config.CheckType]check.Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for t, p := range checkers {
		r.checkers[t] = p
	}
}

func (r *ProberRegistry) Get(t config.CheckType) check.Checker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkers[t]
}

type Scheduler struct {
	cfg      *config.Config
	checks   []config.CheckConfig
	registry *ProberRegistry
	origin   *config.Origin
	results  chan *check.Result
	wg       sync.WaitGroup // tracks per-check loops and one-shot startup runs
}

func New(cfg *config.Config, checks []config.CheckConfig, registry *ProberRegistry, originID string) *Scheduler {
	return &Scheduler{
		cfg:      cfg,
		checks:   checks,
		registry: registry,
		origin:   cfg.ResolveOrigin(originID),
		results:  make(chan *check.Result, 100),
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	for i := range s.checks {
		pc := s.checks[i]

		checker := s.registry.Get(pc.Type)
		if checker == nil {
			slog.Warn("No checker registered for type, skipping", "type", pc.Type, "name", pc.Name)
			continue
		}

		if !gronx.IsValid(pc.Schedule) {
			slog.Error("Invalid schedule, skipping", "name", pc.Name, "schedule", pc.Schedule)
			continue
		}

		staggerDelay := ComputeStagger(pc.Name, s.origin)
		if staggerDelay > 0 {
			slog.Info("Staggering check", "name", pc.Name, "delay", staggerDelay)
		}

		s.wg.Add(1)
		go s.runLoop(ctx, checker, pc, staggerDelay)
		slog.Info("Scheduled check", "name", pc.Name, "type", pc.Type, "schedule", pc.Schedule)
	}

	// Run every check once immediately so the status page, metrics, and alert
	// rules have data within seconds of boot instead of waiting up to a full
	// interval for the first scheduled tick (matters most for long-interval
	// checks like cert/domain expiry). Runs are staggered and happen in the
	// background, so Start does not block on slow checks.
	s.runInitial(ctx)

	go func() {
		<-ctx.Done()
		slog.Info("Stopping scheduler")
		s.wg.Wait() // wait for in-flight checks before closing the channel
		close(s.results)
	}()

	return nil
}

// runLoop drives a single check on its cron schedule until ctx is cancelled.
// We own the loop (no external cron library): each iteration computes the next
// matching time with gronx and waits for it. The check runs synchronously, so a
// slow run can only delay or skip the next tick for that one check, never pile
// up overlapping runs of itself.
func (s *Scheduler) runLoop(ctx context.Context, checker check.Checker, cfg config.CheckConfig, stagger time.Duration) {
	defer s.wg.Done()
	for {
		next, err := gronx.NextTick(cfg.Schedule, false)
		if err != nil {
			slog.Error("Failed to compute next tick, stopping check loop", "name", cfg.Name, "schedule", cfg.Schedule, "error", err)
			return
		}

		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		// Stagger spreads check starts within the tick window across origins to
		// avoid a thundering herd; skip the run if shutdown begins mid-stagger.
		if stagger > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(stagger):
			}
		}

		slog.Debug("Running check", "name", cfg.Name, "type", cfg.Type)
		s.execute(ctx, checker, &cfg)
	}
}

// execute runs a single check (with retry), annotates the result, records
// metrics, and publishes it. Shared by the recurring cron job and the
// one-shot startup run so both paths behave identically.
func (s *Scheduler) execute(ctx context.Context, checker check.Checker, cfg *config.CheckConfig) {
	result := runWithRetry(ctx, checker, cfg, s.origin)
	result.Group = cfg.Group
	result.Target = cfg.Target()

	// Mark as degraded if latency exceeds threshold
	if cfg.DegradedAfter > 0 && result.Success && result.DegradedLatency() > cfg.DegradedAfter {
		result.Degraded = true
	}

	metrics.RecordResult(result)
	select {
	case s.results <- result:
	default:
		slog.Warn("Result channel full, dropping result", "name", cfg.Name)
	}
}

// runInitial executes every check once on startup, each after its stagger
// delay, in the background. The schedule loops run independently; a check may
// therefore run once here and again on its first scheduled tick, which is
// harmless for these read-only probes. Runs are WaitGroup-tracked so shutdown
// waits for them before closing the results channel.
func (s *Scheduler) runInitial(ctx context.Context) {
	for i := range s.checks {
		checker := s.registry.Get(s.checks[i].Type)
		if checker == nil {
			continue // already warned about during schedule registration
		}
		checkCfg := s.checks[i] // capture for closure
		delay := ComputeStagger(checkCfg.Name, s.origin)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
			slog.Debug("Running initial check", "name", checkCfg.Name, "type", checkCfg.Type)
			s.execute(ctx, checker, &checkCfg)
		}()
	}
}

func (s *Scheduler) Results() <-chan *check.Result {
	return s.results
}

// runWithRetry executes a check with optional retry policy.
func runWithRetry(ctx context.Context, checker check.Checker, cfg *config.CheckConfig, origin *config.Origin) *check.Result {
	result := checker.Run(ctx, cfg, origin)
	if result.Success || cfg.Retry == nil || cfg.Retry.Count <= 0 {
		return result
	}

	delay := cfg.Retry.Delay
	if delay == 0 {
		delay = 1 * time.Second
	}

	for attempt := 1; attempt <= cfg.Retry.Count; attempt++ {
		slog.Info("Retrying check", "name", cfg.Name, "attempt", attempt, "delay", delay)

		select {
		case <-ctx.Done():
			return result
		case <-time.After(delay):
		}

		result = checker.Run(ctx, cfg, origin)
		result.Retries = attempt
		if result.Success {
			return result
		}

		// Scale delay for next attempt
		switch cfg.Retry.Backoff {
		case "exponential":
			delay *= 2
		case "linear":
			delay += cfg.Retry.Delay
		}
	}

	return result
}
