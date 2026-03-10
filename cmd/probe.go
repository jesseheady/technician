package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/probe"
	"github.com/spf13/cobra"
)

var (
	probeName   string
	probeOutput string
)

var probeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Run probes",
}

var probeRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a single probe or all probes and output results",
	RunE:  runProbe,
}

func init() {
	probeRunCmd.Flags().StringVar(&probeName, "probe", "", "probe name to run (runs all if empty)")
	probeRunCmd.Flags().StringVarP(&probeOutput, "output", "o", "text", "output format: text, json")
	probeCmd.AddCommand(probeRunCmd)
	rootCmd.AddCommand(probeCmd)
}

type probeResultOutput struct {
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

func runProbe(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	probesDir := config.ResolveProbesDir(cfgFile)
	probes, err := config.LoadProbes(probesDir)
	if err != nil {
		return fmt.Errorf("loading probes: %w", err)
	}

	site := cfg.ResolveSite(siteCode)

	probers := newProbers()

	var toRun []config.ProbeConfig
	if probeName != "" {
		p := config.FindProbeByName(probes, probeName)
		if p == nil {
			return fmt.Errorf("probe %q not found", probeName)
		}
		toRun = append(toRun, *p)
	} else {
		toRun = probes
	}

	ctx := context.Background()
	var results []*probe.Result

	for i := range toRun {
		pc := &toRun[i]
		prober, ok := probers[pc.Type]
		if !ok {
			slog.Warn("No prober for type", "type", pc.Type, "name", pc.Name)
			continue
		}
		result := prober.Run(ctx, pc, site)
		results = append(results, result)
	}

	return outputResults(results, probeOutput)
}

func outputResults(results []*probe.Result, format string) error {
	switch strings.ToLower(format) {
	case "json":
		out := make([]probeResultOutput, len(results))
		for i, r := range results {
			out[i] = probeResultOutput{
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
