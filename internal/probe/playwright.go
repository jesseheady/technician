package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

type PlaywrightProber struct {
	RunnerPath string
}

func NewPlaywrightProber(runnerPath string) *PlaywrightProber {
	return &PlaywrightProber{RunnerPath: runnerPath}
}

func (p *PlaywrightProber) Type() config.ProbeType {
	return config.ProbeTypePlaywright
}

type playwrightOutput struct {
	Success       bool       `json:"success"`
	Duration      float64    `json:"duration_ms"`
	Error         string     `json:"error,omitempty"`
	Vitals        *WebVitals `json:"vitals,omitempty"`
	HAR           *HARData   `json:"har,omitempty"`
	VideoPath     string     `json:"video_path,omitempty"`
	ResourceCount int        `json:"resource_count"`
	Logs          []string   `json:"logs,omitempty"`
}

func (p *PlaywrightProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypePlaywright, site)

	if cfg.Playwright == nil {
		result.Error = "missing Playwright probe configuration"
		return result
	}

	pcfg := cfg.Playwright
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		p.RunnerPath,
		"--script", pcfg.Script,
	}
	if pcfg.BaseURL != "" {
		args = append(args, "--base-url", pcfg.BaseURL)
	}
	if pcfg.Video {
		args = append(args, "--video")
	}
	if pcfg.Authenticator != "" {
		args = append(args, "--authenticator", pcfg.Authenticator)
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "node", args...)
	output, err := cmd.Output()
	result.Duration = time.Since(start)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.Error = fmt.Sprintf("playwright runner failed: %s (stderr: %s)", err, string(exitErr.Stderr))
		} else {
			result.Error = fmt.Sprintf("running playwright: %v", err)
		}
		slog.Warn("Playwright probe failed", "name", cfg.Name, "error", result.Error)
		return result
	}

	var pwOutput playwrightOutput
	if err := json.Unmarshal(output, &pwOutput); err != nil {
		result.Error = fmt.Sprintf("parsing playwright output: %v", err)
		return result
	}

	result.Success = pwOutput.Success
	result.WebVitals = pwOutput.Vitals
	result.HARData = pwOutput.HAR
	result.VideoPath = pwOutput.VideoPath
	result.ResourceCount = pwOutput.ResourceCount

	if pwOutput.Error != "" {
		result.Error = pwOutput.Error
	}

	slog.Debug("Playwright probe completed",
		"name", cfg.Name,
		"success", result.Success,
		"duration", result.Duration,
		"resources", result.ResourceCount,
	)

	return result
}
