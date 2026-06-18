package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile  string
	originID string
	logLevel string
)

var rootCmd = &cobra.Command{
	Use:   "technician",
	Short: "Multi-region infrastructure check runner",
	Long:  "Technician is a self-hosted check runner that checks your infrastructure over the network. Deploy one worker per region, point Prometheus at them — eleven check types, performance budgets, OTLP traces, and Grafana dashboards included.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		level := slog.LevelInfo
		if logLevel != "" {
			switch logLevel {
			case "debug":
				level = slog.LevelDebug
			case "info":
				level = slog.LevelInfo
			case "warn":
				level = slog.LevelWarn
			case "error":
				level = slog.LevelError
			}
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
		})))
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "technician.yml", "config file path")
	rootCmd.PersistentFlags().StringVar(&originID, "origin", os.Getenv("ORIGIN_ID"), "origin ID for this instance")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level: debug, info, warn, error")
}

func Execute() error {
	return rootCmd.Execute()
}
