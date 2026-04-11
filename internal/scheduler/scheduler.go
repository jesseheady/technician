package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/metrics"
	"github.com/m0nkey/technician/internal/check"
	"github.com/robfig/cron/v3"
)

type ProberRegistry struct {
	mu      sync.RWMutex
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
	site     *config.Site
	results  chan *check.Result
}

func New(cfg *config.Config, checks []config.CheckConfig, registry *ProberRegistry, siteCode string) *Scheduler {
	c := cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))

	return &Scheduler{
		cron:     c,
		cfg:      cfg,
		checks:   checks,
		registry: registry,
		site:     cfg.ResolveSite(siteCode),
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
		staggerDelay := ComputeStagger(pc.Name, s.site)
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
			result := runWithRetry(ctx, checker, &checkCfg, s.site)
			result.Group = checkCfg.Group
			result.Target = checkCfg.Target()

			// Mark as degraded if duration exceeds threshold
			if checkCfg.DegradedAfter > 0 && result.Success && result.Duration > checkCfg.DegradedAfter {
				result.Degraded = true
			}

			metrics.RecordResult(result)
			select {
			case s.results <- result:
			default:
				slog.Warn("Result channel full, dropping result", "name", checkCfg.Name)
			}
		})
		if err != nil {
			slog.Error("Failed to schedule check", "name", pc.Name, "schedule", schedule, "error", err)
			continue
		}

		slog.Info("Scheduled check", "name", pc.Name, "type", pc.Type, "schedule", schedule)
	}

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

func (s *Scheduler) Results() <-chan *check.Result {
	return s.results
}

// runWithRetry executes a check with optional retry policy.
func runWithRetry(ctx context.Context, checker check.Checker, cfg *config.CheckConfig, site *config.Site) *check.Result {
	result := checker.Run(ctx, cfg, site)
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

		result = checker.Run(ctx, cfg, site)
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
