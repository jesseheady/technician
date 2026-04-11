package check

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

type TracerouteChecker struct{}

func NewTracerouteChecker() *TracerouteChecker {
	return &TracerouteChecker{}
}

func (p *TracerouteChecker) Type() config.CheckType {
	return config.CheckTypeTraceroute
}

func (p *TracerouteChecker) Run(ctx context.Context, cfg *config.CheckConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.CheckTypeTraceroute, site)

	if cfg.Traceroute == nil {
		result.Error = "missing traceroute probe configuration"
		return result
	}

	tcfg := cfg.Traceroute
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use only --json; --report-wide can cause some mtr-tiny builds to print human-readable report to stdout instead of JSON
	args := []string{
		"--json",
		fmt.Sprintf("--max-ttl=%d", tcfg.MaxHops),
		fmt.Sprintf("--report-cycles=%d", tcfg.Count),
		tcfg.Host,
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "mtr", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	result.Duration = time.Since(start)

	if err != nil {
		// mtr often fails with "Permission denied" when not root (raw sockets on macOS/Linux)
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			result.Error = fmt.Sprintf("mtr: %s", msg)
		} else {
			result.Error = fmt.Sprintf("running mtr: %v", err)
		}
		slog.Warn("Traceroute probe failed", "name", cfg.Name, "host", tcfg.Host, "error", err)
		return result
	}

	hops, err := parseMTROutput(output)
	if err != nil {
		result.Error = fmt.Sprintf("parsing mtr output: %v", err)
		return result
	}

	result.Hops = hops
	result.Success = true

	slog.Debug("Traceroute probe completed",
		"name", cfg.Name,
		"host", tcfg.Host,
		"hops", len(hops),
		"duration", result.Duration,
	)

	return result
}

type mtrReport struct {
	Report struct {
		Hubs []mtrHub `json:"hubs"`
	} `json:"report"`
}

type mtrHub struct {
	Count int     `json:"count"`
	Host  string  `json:"host"`
	ASN   int     `json:"ASN"`
	Loss  float64 `json:"Loss%"`
	Avg   float64 `json:"Avg"`
}

func parseMTROutput(data []byte) ([]TracerouteHop, error) {
	// Some mtr builds (e.g. mtr-tiny in Docker) may print progress or other text before the JSON
	jsonStart := bytes.IndexByte(data, '{')
	if jsonStart < 0 {
		snippet := string(bytes.TrimSpace(data))
		if len(snippet) > 80 {
			snippet = snippet[:80] + " ..."
		}
		return nil, fmt.Errorf("unmarshal mtr JSON: no JSON object found in output (mtr may need root or use a different format); first bytes: %q", snippet)
	}
	data = data[jsonStart:]

	var report mtrReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("unmarshal mtr JSON: %w", err)
	}

	hops := make([]TracerouteHop, len(report.Report.Hubs))
	for i, hub := range report.Report.Hubs {
		hops[i] = TracerouteHop{
			Hop:         i + 1,
			Host:        hub.Host,
			ASN:         hub.ASN,
			AvgMs:       hub.Avg,
			LossPercent: hub.Loss,
		}
	}

	return hops, nil
}
