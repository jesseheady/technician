package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile  string
	siteCode string
	verbose  bool
)

var rootCmd = &cobra.Command{
	Use:   "technician",
	Short: "Synthetic monitoring orchestrator",
	Long:  "Technician is a synthetic monitoring orchestrator that runs health check probes from multiple geographic sites, exporting rich metrics to Prometheus and traces via OTLP.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
		})))
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "technician.yml", "config file path")
	rootCmd.PersistentFlags().StringVar(&siteCode, "site", os.Getenv("SITE_CODE"), "site code for this instance")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
}

func Execute() error {
	return rootCmd.Execute()
}
