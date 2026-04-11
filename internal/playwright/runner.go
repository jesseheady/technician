package playwright

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/m0nkey/technician/internal/check"
)

//go:embed scripts/*
var embeddedScripts embed.FS

type Runner struct {
	scriptsDir string
}

func NewRunner() (*Runner, error) {
	// Extract embedded scripts to a temp dir if needed
	tmpDir := filepath.Join(os.TempDir(), "technician-playwright")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating scripts dir: %w", err)
	}

	// Write run.js if not already present
	runJS := filepath.Join(tmpDir, "run.js")
	if _, err := os.Stat(runJS); os.IsNotExist(err) {
		data, err := embeddedScripts.ReadFile("scripts/run.js")
		if err != nil {
			return nil, fmt.Errorf("reading embedded run.js: %w", err)
		}
		if err := os.WriteFile(runJS, data, 0o644); err != nil {
			return nil, fmt.Errorf("writing run.js: %w", err)
		}
	}

	return &Runner{scriptsDir: tmpDir}, nil
}

func (r *Runner) RunnerPath() string {
	return filepath.Join(r.scriptsDir, "run.js")
}

type RunConfig struct {
	Script        string `json:"script"`
	BaseURL       string `json:"base_url,omitempty"`
	Video         bool   `json:"video"`
	Authenticator string `json:"authenticator,omitempty"`
	Network       string `json:"network,omitempty"`
	Device        string `json:"device,omitempty"`
	Timeout       int    `json:"timeout_ms"`
}

type RunResult struct {
	Success       bool             `json:"success"`
	DurationMs    float64          `json:"duration_ms"`
	Error         string           `json:"error,omitempty"`
	Vitals        *check.WebVitals `json:"vitals,omitempty"`
	HAR           *check.HARData   `json:"har,omitempty"`
	VideoPath     string           `json:"video_path,omitempty"`
	ResourceCount int              `json:"resource_count"`
	Logs          []string         `json:"logs,omitempty"`
}

func (r *Runner) Run(ctx context.Context, cfg RunConfig) (*RunResult, error) {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", r.RunnerPath(), string(configJSON))
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			slog.Error("Playwright runner failed", "stderr", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("running playwright: %w", err)
	}

	var result RunResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parsing result: %w", err)
	}

	return &result, nil
}
