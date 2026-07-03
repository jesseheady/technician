package playwright

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// stubNode puts a fake `node` executable on PATH that writes its config
// argument (argv[2]) to capturePath and prints the given JSON result.
func stubNode(t *testing.T, capturePath, resultJSON string, exitCode int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH stub uses a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s' \"$2\" > \"" + capturePath + "\"\nprintf '%s' '" + resultJSON + "'\n"
	if exitCode != 0 {
		script += "exit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "node"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// capturedWorkDir reads the config JSON the stub captured and returns work_dir.
func capturedWorkDir(t *testing.T, capturePath string) string {
	t.Helper()
	data, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("stub did not capture config: %v", err)
	}
	var cfg RunConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("captured config is not valid JSON: %v", err)
	}
	if cfg.WorkDir == "" {
		t.Fatal("expected work_dir to be set in the runner config")
	}
	return cfg.WorkDir
}

func TestRunCreatesAndRemovesWorkDir(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "config.json")
	stubNode(t, capture, `{"success":true,"duration_ms":1,"video_path":"/some/video.webm"}`, 0)

	r := &Runner{scriptsDir: t.TempDir()}
	result, err := r.Run(context.Background(), RunConfig{Script: "probe.js"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Error("expected success result")
	}

	workDir := capturedWorkDir(t, capture)
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("expected work dir %s to be removed after Run", workDir)
	}
	if result.VideoPath != "" {
		t.Errorf("expected VideoPath cleared (video deleted with work dir), got %q", result.VideoPath)
	}
}

func TestRunRemovesWorkDirOnFailure(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "config.json")
	stubNode(t, capture, `{}`, 1)

	r := &Runner{scriptsDir: t.TempDir()}
	if _, err := r.Run(context.Background(), RunConfig{Script: "probe.js"}); err == nil {
		t.Fatal("expected error from failing runner")
	}

	workDir := capturedWorkDir(t, capture)
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("expected work dir %s to be removed after failed Run", workDir)
	}
}
