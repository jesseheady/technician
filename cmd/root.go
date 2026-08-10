package cmd

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/jesseheady/technician/internal/config"
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
	Long:  "Technician is a self-hosted check runner that checks your infrastructure over the network. Deploy one worker per region, point Prometheus at them — many check types, performance budgets, OTLP traces, and Grafana dashboards included.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Baseline logger from the flag alone; config-driven format/level is
		// applied by applyLogConfig once the config file has been loaded.
		setupLogging("", logLevel)
	},
}

// parseLevel maps a level string to slog.Level, defaulting to info.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// newLogHandler builds a slog handler: JSON (Loki-native) when format is "json",
// text otherwise.
func newLogHandler(w io.Writer, format, level string) slog.Handler {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	if strings.EqualFold(format, "json") {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// setupLogging installs the default logger for the given format and level.
func setupLogging(format, level string) {
	slog.SetDefault(slog.New(newLogHandler(os.Stderr, format, level)))
}

// applyLogConfig re-installs the logger from config after it is loaded. The
// --log-level flag, if set, still wins over logging.level.
func applyLogConfig(cfg *config.Config) {
	level := logLevel
	if level == "" {
		level = cfg.Logging.Level
	}
	setupLogging(cfg.Logging.Format, level)
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "technician.yml", "config file path")
	rootCmd.PersistentFlags().StringVar(&originID, "origin", os.Getenv("ORIGIN_ID"), "origin ID for this instance")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level: debug, info, warn, error")
}

func Execute() error {
	return rootCmd.Execute()
}
