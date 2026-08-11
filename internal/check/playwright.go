package check

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

type PlaywrightChecker struct {
	RunnerPath string
	// ServerURL, when set, makes the runner connect to a remote Playwright
	// server instead of launching a local browser.
	ServerURL   string
	browserSem  chan struct{} // limits concurrent Chromium instances
	maxBrowsers int
}

// NewPlaywrightChecker creates a Playwright checker with a concurrency limit.
// maxBrowsers controls how many browsers can run simultaneously; it bounds
// local Chromium processes and, in managed mode, concurrent sessions against
// the remote server. If maxBrowsers <= 0, it defaults to 2. An empty serverURL
// means launch locally.
func NewPlaywrightChecker(runnerPath string, maxBrowsers int, serverURL string) *PlaywrightChecker {
	if maxBrowsers <= 0 {
		maxBrowsers = 2
	}
	return &PlaywrightChecker{
		RunnerPath:  runnerPath,
		ServerURL:   serverURL,
		browserSem:  make(chan struct{}, maxBrowsers),
		maxBrowsers: maxBrowsers,
	}
}

func (p *PlaywrightChecker) Type() config.CheckType {
	return config.CheckTypePlaywright
}

type playwrightOutput struct {
	Success       bool       `json:"success"`
	Infra         bool       `json:"infra,omitempty"`
	Duration      float64    `json:"duration_ms"`
	Error         string     `json:"error,omitempty"`
	Vitals        *WebVitals `json:"vitals,omitempty"`
	HAR           *HARData   `json:"har,omitempty"`
	VideoPath     string     `json:"video_path,omitempty"`
	ResourceCount int        `json:"resource_count"`
	Logs          []string   `json:"logs,omitempty"`
}

func (p *PlaywrightChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypePlaywright, origin)

	if cfg.Playwright == nil {
		result.InfraError = true
		result.Error = "missing Playwright check configuration"
		return result
	}

	pcfg := cfg.Playwright
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Acquire browser slot (respects context cancellation)
	select {
	case p.browserSem <- struct{}{}:
		defer func() { <-p.browserSem }()
	case <-ctx.Done():
		result.InfraError = true
		result.Error = fmt.Sprintf("timed out waiting for browser slot (%d/%d in use)", len(p.browserSem), p.maxBrowsers)
		slog.Warn("Playwright check queued too long", "name", cfg.Name, "max_browsers", p.maxBrowsers)
		return result
	}

	// Per-run scratch dir for HAR and video output. A unique dir per run
	// prevents concurrent checks (max_browsers > 1) from clobbering each
	// other's HAR files. run.js parses the HAR into its JSON output before
	// returning, and nothing persists videos yet, so the whole dir is removed
	// when the run finishes — even on error.
	workDir, err := os.MkdirTemp("", "technician-pw-")
	if err != nil {
		result.InfraError = true
		result.Error = fmt.Sprintf("creating playwright work dir: %v", err)
		return result
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			slog.Warn("Failed to remove playwright work dir", "dir", workDir, "error", err)
		}
	}()

	runnerConfig := map[string]interface{}{
		"script":   pcfg.Script,
		"work_dir": workDir,
	}
	if pcfg.BaseURL != "" {
		runnerConfig["base_url"] = pcfg.BaseURL
	}
	if pcfg.Video {
		runnerConfig["video"] = true
	}
	if pcfg.Authenticator != "" {
		runnerConfig["authenticator"] = pcfg.Authenticator
	}
	if pcfg.Network != "" {
		runnerConfig["network"] = pcfg.Network
	}
	if pcfg.Device != "" {
		runnerConfig["device"] = pcfg.Device
	}
	if p.ServerURL != "" {
		runnerConfig["server_url"] = p.ServerURL
	}

	configJSON, err := json.Marshal(runnerConfig)
	if err != nil {
		result.InfraError = true
		result.Error = fmt.Sprintf("marshaling runner config: %v", err)
		return result
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "node", p.RunnerPath, string(configJSON))
	cmd.Env = playwrightEnv()
	output, err := cmd.Output()
	result.Duration = time.Since(start)

	if err != nil {
		result.InfraError = true
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.Error = fmt.Sprintf("playwright runner failed: %s (stderr: %s)", err, string(exitErr.Stderr))
		} else {
			result.Error = fmt.Sprintf("running playwright: %v", err)
		}
		slog.Warn("Playwright check infra error", "name", cfg.Name, "error", result.Error)
		return result
	}

	var pwOutput playwrightOutput
	if err := json.Unmarshal(output, &pwOutput); err != nil {
		result.InfraError = true
		result.Error = fmt.Sprintf("parsing playwright output: %v", err)
		return result
	}

	result.Success = pwOutput.Success
	// A setup-stage failure (browser launch, context, or probe load) means the
	// runner itself is broken — treat it as an infra error so --fail-on-error
	// catches it. Probe/navigation failures against the target are not infra.
	if pwOutput.Infra {
		result.InfraError = true
		slog.Warn("Playwright check infra error", "name", cfg.Name, "error", pwOutput.Error)
	}
	result.WebVitals = pwOutput.Vitals
	result.HARData = pwOutput.HAR
	result.ResourceCount = pwOutput.ResourceCount

	// The video file lives in the work dir and is deleted with it. Leave the
	// path empty rather than return a dangling reference; retaining videos
	// requires routing them through the artifact store (#218).
	if pwOutput.VideoPath != "" {
		slog.Debug("Playwright video discarded (no artifact store wired)", "name", cfg.Name, "path", pwOutput.VideoPath)
	}

	if pcfg.Network != "" {
		result.Labels["network"] = pcfg.Network
	}
	if pcfg.Device != "" {
		result.Labels["device"] = pcfg.Device
	}

	if pwOutput.Error != "" {
		result.Error = pwOutput.Error
	}

	slog.Debug("Playwright check completed",
		"name", cfg.Name,
		"success", result.Success,
		"duration", result.Duration,
		"resources", result.ResourceCount,
	)

	return result
}

// playwrightEnv returns the process environment with NODE_PATH set to the local
// node_modules directory when not already configured externally (CI, Docker).
func playwrightEnv() []string {
	env := os.Environ()
	for _, e := range env {
		if len(e) > 10 && e[:10] == "NODE_PATH=" {
			return env
		}
	}
	localModules := filepath.Join("internal", "playwright", "scripts", "node_modules")
	if abs, err := filepath.Abs(localModules); err == nil {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return append(env, "NODE_PATH="+abs)
		}
	}
	return env
}
