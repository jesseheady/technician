package cmd

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/m0nkey/technician/internal/budget"
	"github.com/m0nkey/technician/internal/config"
	"github.com/m0nkey/technician/internal/exporter"
	"github.com/m0nkey/technician/internal/metrics"
	"github.com/m0nkey/technician/internal/notify"
	"github.com/m0nkey/technician/internal/scheduler"
	"github.com/m0nkey/technician/internal/server"
	"github.com/m0nkey/technician/internal/status"
	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run as a long-running worker with scheduled probes and /metrics endpoint",
	RunE:  runWorker,
}

func init() {
	rootCmd.AddCommand(workerCmd)
}

func runWorker(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	probesDir := config.ResolveProbesDir(cfgFile)
	probes, err := config.LoadProbes(probesDir)
	if err != nil {
		slog.Warn("No probes loaded", "error", err)
		probes = nil
	}

	// Load budgets (optional — not an error if missing)
	budgets, err := budget.LoadBudgetsFromDir(cfgFile)
	if err != nil {
		slog.Info("No budgets loaded", "error", err)
	} else {
		slog.Info("Loaded budgets", "count", len(budgets))
	}

	slog.Info("Loaded configuration",
		"sites", len(cfg.Sites),
		"probes", len(probes),
		"site", siteCode,
		"listen", cfg.Metrics.Prometheus.Listen,
	)

	registry := scheduler.NewProberRegistry()
	registry.RegisterMap(newProbers(cfg))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched := scheduler.New(cfg, probes, registry, siteCode)
	if err := sched.Start(ctx); err != nil {
		return err
	}

	dataDir := os.Getenv("TECHNICIAN_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/technician"
	}
	store := status.NewStore(cfg.Service, cfg.ResolveSite(siteCode), filepath.Join(dataDir, "status.json"))

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/probe", exporter.NewBlackboxHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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
	site := cfg.ResolveSite(siteCode)
	go func() {
		for result := range sched.Results() {
			store.Push(result)
			notifier.HandleResult(ctx, result)
			notifier.HandleCertResult(ctx, result)

			if len(budgets) > 0 && !result.InfraError {
				for _, c := range budget.EvaluateAll(result, budgets) {
					metrics.RecordBudgetViolation(c.Probe, c.Metric, c.Violated, site)
					crossedThreshold := store.RecordBudgetCheck(c.Probe, c.Metric, c.Violated)
					// Only send webhook when crossing the fail threshold
					if crossedThreshold {
						notifier.HandleBudgetViolation(ctx, c.Probe, c.Metric, true)
					}
					if c.Violated {
						slog.Warn("Budget violation",
							"probe", c.Probe,
							"metric", c.Metric,
							"actual", c.Actual,
							"threshold", c.Threshold,
						)
					}
				}
			}

			level := slog.LevelInfo
			if !result.Success {
				level = slog.LevelWarn
			}
			slog.Log(ctx, level, "Probe result",
				"name", result.Name,
				"type", result.Type,
				"success", result.Success,
				"duration", result.Duration,
				"error", result.Error,
			)
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
				store.Save()
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
	store.Save()
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return httpServer.Shutdown(shutdownCtx)
}
