package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jesseheady/technician/internal/budget"
	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/exporter"
	"github.com/jesseheady/technician/internal/metrics"
	"github.com/jesseheady/technician/internal/notify"
	"github.com/jesseheady/technician/internal/scheduler"
	"github.com/jesseheady/technician/internal/server"
	"github.com/jesseheady/technician/internal/status"
	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run as a long-running worker with scheduled checks and /metrics endpoint",
	RunE:  runWorker,
}

func init() {
	addFilterFlags(workerCmd)
	rootCmd.AddCommand(workerCmd)
}

func runWorker(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	applyLogConfig(cfg)

	checks, err := loadFilteredChecks(cfg)
	if err != nil {
		slog.Warn("No checks loaded", "error", err)
		checks = nil
	}

	// Load budgets (optional — not an error if missing)
	budgets, err := budget.LoadBudgetsFromDir(cfgFile)
	if err != nil {
		slog.Info("No budgets loaded", "error", err)
	} else {
		slog.Info("Loaded budgets", "count", len(budgets))
	}

	metrics.SetMaxCheckCardinality(cfg.Metrics.Prometheus.MaxCheckCardinality)

	slog.Info("Loaded configuration",
		"origins", len(cfg.Origins),
		"checks", len(checks),
		"origin", originID,
		"listen", cfg.Metrics.Prometheus.Listen,
		"max_check_cardinality", cfg.Metrics.Prometheus.MaxCheckCardinality,
	)

	registry := scheduler.NewCheckerRegistry()
	registry.RegisterMap(newCheckers(cfg))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Tracing is a no-op unless metrics.otel.endpoint is set.
	shutdownOTEL, err := metrics.InitOTEL(ctx, &cfg.Metrics.OTEL, cfg.Service)
	if err != nil {
		return fmt.Errorf("initializing OTLP tracing: %w", err)
	}
	defer func() {
		// The run context is cancelled by the time this fires, so give the
		// batch exporter its own deadline to flush pending spans.
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		if err := shutdownOTEL(flushCtx); err != nil {
			slog.Warn("OTLP shutdown failed", "error", err)
		}
	}()

	sched := scheduler.New(cfg, checks, registry, originID)
	if err := sched.Start(ctx); err != nil {
		return err
	}

	dataDir := os.Getenv("TECHNICIAN_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/technician"
	}
	store := status.NewStore(cfg.Service, cfg.ResolveOrigin(originID), filepath.Join(dataDir, "status.json"))
	store.Reconcile(checks)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/probe", exporter.NewBlackboxHandler()) // Blackbox Exporter API contract
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !store.WriteHealthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "status store writes failing: %d consecutive failures\n", store.ConsecutiveWriteFailures())
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.Handle("/api/", status.Handler(store))
	mux.Handle("/", status.Handler(store))

	httpServer := &http.Server{
		Addr:           cfg.Metrics.Prometheus.Listen,
		Handler:        server.Gzip(mux),
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second, // allow time for /probe endpoint (up to 30s)
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	go func() {
		slog.Info("Starting server", "addr", cfg.Metrics.Prometheus.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
		}
	}()

	// Webhook notifications (nil if none configured)
	notifier := notify.NewManager(cfg.Webhooks)

	// Drain results: store + budget evaluation + notifications + log
	origin := cfg.ResolveOrigin(originID)
	go func() {
		for result := range sched.Results() {
			store.Push(result)
			traceID, spanID := metrics.TraceCheckResult(ctx, result)
			notifier.HandleResult(ctx, result)
			notifier.HandleCertResult(ctx, result)

			if len(budgets) > 0 && !result.InfraError {
				for _, c := range budget.EvaluateAll(result, budgets) {
					metrics.RecordBudgetViolation(c.Check, c.Metric, c.Violated, origin)
					crossedThreshold := store.RecordBudgetCheck(c.Check, c.Metric, c.Violated)
					// Only send webhook when crossing the fail threshold
					if crossedThreshold {
						notifier.HandleBudgetViolation(ctx, c.Check, c.Metric, true)
					}
					if c.Violated {
						slog.Warn("Budget violation",
							"check", c.Check,
							"metric", c.Metric,
							"actual", c.Actual,
							"threshold", c.Threshold,
						)
					}
				}
			}

			level := slog.LevelInfo
			if !result.Success || result.Degraded {
				level = slog.LevelWarn
			}
			attrs := []any{
				"name", result.Name,
				"type", result.Type,
				"success", result.Success,
				"duration", result.Duration,
				"region", origin.ID,
				"degraded", result.Degraded,
				"retries", result.Retries,
				"error", result.Error,
			}
			// Stamp trace/span IDs when OTLP tracing is enabled so Loki logs
			// link to their trace (Loki↔Tempo correlation).
			if traceID != "" {
				attrs = append(attrs, "trace_id", traceID, "span_id", spanID)
			}
			slog.Log(ctx, level, "Check result", attrs...)
		}
	}()

	// Periodically persist status store + daily backup
	go func() {
		saveTicker := time.NewTicker(60 * time.Second)
		backupTicker := time.NewTicker(1 * time.Hour) // checks daily (idempotent)
		defer saveTicker.Stop()
		defer backupTicker.Stop()
		for {
			select {
			case <-saveTicker.C:
				if err := store.Save(); err != nil {
					metrics.RecordStatusStoreWriteError()
				}
			case <-backupTicker.C:
				store.SaveBackup()
			case <-ctx.Done():
				return
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("Shutting down")
	notifier.Wait()
	if err := store.Save(); err != nil {
		metrics.RecordStatusStoreWriteError()
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return httpServer.Shutdown(shutdownCtx)
}
