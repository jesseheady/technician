package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/metrics"
	"github.com/robfig/cron/v3"
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
	cron     *cron.Cron
	cfg      *config.Config
	checks   []config.CheckConfig
	registry *ProberRegistry
	origin   *config.Origin
	results  chan *check.Result
}

func New(cfg *config.Config, checks []config.CheckConfig, registry *ProberRegistry, originID string) *Scheduler {
	c := cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))

	return &Scheduler{
		cron:     c,
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

		schedule := pc.Schedule
		staggerDelay := ComputeStagger(pc.Name, s.origin)
		if staggerDelay > 0 {
			slog.Info("Staggering check", "name", pc.Name, "delay", staggerDelay)
		}

		checkCfg := pc // capture for closure
		checkDelay := staggerDelay
		_, err := s.cron.AddFunc(schedule, func() {
			if checkDelay > 0 {
				// Use a timer instead of blocking the cron goroutine directly.
				// The sleep is intentional: it spreads check starts within
				// the cron tick window to avoid thundering-herd effects.
				time.Sleep(checkDelay)
			}
			slog.Debug("Running check", "name", checkCfg.Name, "type", checkCfg.Type)
			s.execute(ctx, checker, &checkCfg)
		})
		if err != nil {
			slog.Error("Failed to schedule check", "name", pc.Name, "schedule", schedule, "error", err)
			continue
		}

		slog.Info("Scheduled check", "name", pc.Name, "type", pc.Type, "schedule", schedule)
	}

	// Run every check once immediately so the status page, metrics, and alert
	// rules have data within seconds of boot instead of waiting up to a full
	// interval for the first cron tick (matters most for long-interval checks
	// like cert/domain expiry). Runs are staggered and happen in the
	// background, so Start does not block on slow checks.
	s.runInitial(ctx)

	s.cron.Start()

	go func() {
		<-ctx.Done()
		slog.Info("Stopping scheduler")
		cronCtx := s.cron.Stop()
		<-cronCtx.Done() // wait for running jobs to finish
		close(s.results)
	}()

	return nil
}

// execute runs a single check (with retry), annotates the result, records
// metrics, and publishes it. Shared by the recurring cron job and the
// one-shot startup run so both paths behave identically.
func (s *Scheduler) execute(ctx context.Context, checker check.Checker, cfg *config.CheckConfig) {
	result := runWithRetry(ctx, checker, cfg, s.origin)
	result.Group = cfg.Group
	result.Target = cfg.Target()

	// Mark as degraded if duration exceeds threshold
	if cfg.DegradedAfter > 0 && result.Success && result.Duration > cfg.DegradedAfter {
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
// delay, in the background. The cron schedule continues independently; a
// check may therefore run once here and again on its first cron tick, which
// is harmless for these read-only probes.
func (s *Scheduler) runInitial(ctx context.Context) {
	for i := range s.checks {
		checker := s.registry.Get(s.checks[i].Type)
		if checker == nil {
			continue // already warned about during cron registration
		}
		checkCfg := s.checks[i] // capture for closure
		delay := ComputeStagger(checkCfg.Name, s.origin)
		go func() {
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
