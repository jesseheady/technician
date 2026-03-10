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

	"github.com/monkeyWzr/technician/internal/budget"
	"github.com/monkeyWzr/technician/internal/config"
	"github.com/monkeyWzr/technician/internal/exporter"
	"github.com/monkeyWzr/technician/internal/metrics"
	"github.com/monkeyWzr/technician/internal/scheduler"
	"github.com/monkeyWzr/technician/internal/status"
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
	registry.RegisterMap(newProbers())

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

	server := &http.Server{
		Addr:    cfg.Metrics.Prometheus.Listen,
		Handler: mux,
	}

	go func() {
		slog.Info("Starting server", "addr", cfg.Metrics.Prometheus.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
		}
	}()

	// Drain results: store + budget evaluation + log
	site := cfg.ResolveSite(siteCode)
	go func() {
		for result := range sched.Results() {
			store.Push(result)

			if len(budgets) > 0 && !result.InfraError {
				for _, c := range budget.EvaluateAll(result, budgets) {
					metrics.RecordBudgetViolation(c.Probe, c.Metric, c.Violated, site)
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

	// Periodically persist status store
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				store.Save()
			case <-ctx.Done():
				return
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("Shutting down")
	store.Save()
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return server.Shutdown(shutdownCtx)
}
