package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
	"github.com/spf13/cobra"
)

var (
	checkName   string
	checkOutput string
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Run checks",
}

var checkRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a single check or all checks and output results",
	RunE:  runCheck,
}

func init() {
	checkRunCmd.Flags().StringVar(&checkName, "check", "", "check name to run (runs all if empty)")
	checkRunCmd.Flags().StringVarP(&checkOutput, "output", "o", "text", "output format: text, json")
	addFilterFlags(checkRunCmd)
	checkCmd.AddCommand(checkRunCmd)
	rootCmd.AddCommand(checkCmd)
}

type checkResultOutput struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Success   bool    `json:"success"`
	Duration  float64 `json:"duration_seconds"`
	Error     string  `json:"error,omitempty"`
	Status    int     `json:"status_code,omitempty"`
	DNS       float64 `json:"dns_seconds,omitempty"`
	TLS       float64 `json:"tls_seconds,omitempty"`
	Connect   float64 `json:"connect_seconds,omitempty"`
	TTFB      float64 `json:"ttfb_seconds,omitempty"`
	Transfer  float64 `json:"transfer_seconds,omitempty"`
	BodyBytes int64   `json:"response_bytes,omitempty"`
}

func runCheck(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	applyLogConfig(cfg)

	origin := cfg.ResolveOrigin(originID)

	checkers := newCheckers(cfg)

	var toRun []config.CheckConfig
	if checkName != "" {
		// An explicit --check is direct intent, so search the full set — a
		// named check is never hidden by check_filter.
		checks, err := config.LoadChecks(config.ResolveChecksPath(cfgFile))
		if err != nil {
			return fmt.Errorf("loading checks: %w", err)
		}
		p := config.FindCheckByName(checks, checkName)
		if p == nil {
			return fmt.Errorf("check %q not found", checkName)
		}
		toRun = append(toRun, *p)
	} else {
		checks, err := loadFilteredChecks(cfg)
		if err != nil {
			return err
		}
		toRun = checks
	}

	ctx := context.Background()
	var results []*check.Result

	for i := range toRun {
		pc := &toRun[i]
		checker, ok := checkers[pc.Type]
		if !ok {
			slog.Warn("No checker for type", "type", pc.Type, "name", pc.Name)
			continue
		}
		result := checker.Run(ctx, pc, origin)
		results = append(results, result)
	}

	return outputResults(results, checkOutput)
}

func outputResults(results []*check.Result, format string) error {
	switch strings.ToLower(format) {
	case "json":
		out := make([]checkResultOutput, len(results))
		for i, r := range results {
			out[i] = checkResultOutput{
				Name:      r.Name,
				Type:      string(r.Type),
				Success:   r.Success,
				Duration:  r.Duration.Seconds(),
				Error:     r.Error,
				Status:    r.StatusCode,
				DNS:       r.DNSDuration.Seconds(),
				TLS:       r.TLSDuration.Seconds(),
				Connect:   r.ConnDuration.Seconds(),
				TTFB:      r.TTFBDuration.Seconds(),
				Transfer:  r.TransferDuration.Seconds(),
				BodyBytes: r.ResponseBytes,
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)

	default:
		for _, r := range results {
			status := "UP"
			if !r.Success {
				status = "DOWN"
			}
			fmt.Printf("[%s] %s (%s) — %s (%.3fs)",
				status, r.Name, r.Type, r.Duration.Round(time.Millisecond), r.Duration.Seconds())
			if r.Error != "" {
				fmt.Printf(" error=%s", r.Error)
			}
			if r.StatusCode > 0 {
				fmt.Printf(" status=%d", r.StatusCode)
			}
			fmt.Println()
		}
		return nil
	}
}
