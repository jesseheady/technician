package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/metrics"
	"github.com/jesseheady/technician/internal/probe"
	"github.com/robfig/cron/v3"
)

type ProberRegistry struct {
	mu      sync.RWMutex
	probers map[config.ProbeType]probe.Prober
}

func NewProberRegistry() *ProberRegistry {
	return &ProberRegistry{
		probers: make(map[config.ProbeType]probe.Prober),
	}
}

func (r *ProberRegistry) Register(p probe.Prober) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probers[p.Type()] = p
}

func (r *ProberRegistry) RegisterMap(probers map[config.ProbeType]probe.Prober) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for t, p := range probers {
		r.probers[t] = p
	}
}

func (r *ProberRegistry) Get(t config.ProbeType) probe.Prober {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.probers[t]
}

type Scheduler struct {
	cron     *cron.Cron
	cfg      *config.Config
	probes   []config.ProbeConfig
	registry *ProberRegistry
	site     *config.Site
	results  chan *probe.Result
}

func New(cfg *config.Config, probes []config.ProbeConfig, registry *ProberRegistry, siteCode string) *Scheduler {
	c := cron.New(cron.WithSeconds(), cron.WithLogger(cron.DefaultLogger))

	return &Scheduler{
		cron:     c,
		cfg:      cfg,
		probes:   probes,
		registry: registry,
		site:     cfg.ResolveSite(siteCode),
		results:  make(chan *probe.Result, 100),
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	for i := range s.probes {
		pc := s.probes[i]

		prober := s.registry.Get(pc.Type)
		if prober == nil {
			slog.Warn("No prober registered for type, skipping", "type", pc.Type, "name", pc.Name)
			continue
		}

		schedule := pc.Schedule
		staggerDelay := ComputeStagger(pc.Name, s.site)
		if staggerDelay > 0 {
			slog.Info("Staggering probe", "name", pc.Name, "delay", staggerDelay)
		}

		delay := staggerDelay
		probeCfg := pc // capture for closure
		_, err := s.cron.AddFunc(schedule, func() {
			if delay > 0 {
				time.Sleep(delay)
			}
			slog.Debug("Running probe", "name", probeCfg.Name, "type", probeCfg.Type)
			result := runWithRetry(ctx, prober, &probeCfg, s.site)
			result.Group = probeCfg.Group

			// Mark as degraded if duration exceeds threshold
			if probeCfg.DegradedAfter > 0 && result.Success && result.Duration > probeCfg.DegradedAfter {
				result.Degraded = true
			}

			metrics.RecordResult(result)
			select {
			case s.results <- result:
			default:
				slog.Warn("Result channel full, dropping result", "name", probeCfg.Name)
			}
		})
		if err != nil {
			slog.Error("Failed to schedule probe", "name", pc.Name, "schedule", schedule, "error", err)
			continue
		}

		slog.Info("Scheduled probe", "name", pc.Name, "type", pc.Type, "schedule", schedule)
	}

	s.cron.Start()

	go func() {
		<-ctx.Done()
		slog.Info("Stopping scheduler")
		s.cron.Stop()
	}()

	return nil
}

func (s *Scheduler) Results() <-chan *probe.Result {
	return s.results
}

// runWithRetry executes a probe with optional retry policy.
func runWithRetry(ctx context.Context, prober probe.Prober, cfg *config.ProbeConfig, site *config.Site) *probe.Result {
	result := prober.Run(ctx, cfg, site)
	if result.Success || cfg.Retry == nil || cfg.Retry.Count <= 0 {
		return result
	}

	delay := cfg.Retry.Delay
	if delay == 0 {
		delay = 1 * time.Second
	}

	for attempt := 1; attempt <= cfg.Retry.Count; attempt++ {
		slog.Info("Retrying probe", "name", cfg.Name, "attempt", attempt, "delay", delay)

		select {
		case <-ctx.Done():
			return result
		case <-time.After(delay):
		}

		result = prober.Run(ctx, cfg, site)
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
