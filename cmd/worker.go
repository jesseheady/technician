package cmd

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/exporter"
	"github.com/jesseheady/technician/internal/metrics"
	"github.com/jesseheady/technician/internal/scheduler"
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

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/probe", exporter.NewBlackboxHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:    cfg.Metrics.Prometheus.Listen,
		Handler: mux,
	}

	go func() {
		slog.Info("Starting metrics server", "addr", cfg.Metrics.Prometheus.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Metrics server error", "error", err)
		}
	}()

	// Drain results (log them)
	go func() {
		for result := range sched.Results() {
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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("Shutting down")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return server.Shutdown(shutdownCtx)
}
