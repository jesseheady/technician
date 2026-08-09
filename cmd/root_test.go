package cmd

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"WARN":  slog.LevelWarn, // case-insensitive
		"":      slog.LevelInfo, // default
		"bogus": slog.LevelInfo, // unknown falls back to info
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewLogHandlerJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(&buf, "json", "info"))
	logger.Info("hello", "check", "acme")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("json format did not produce valid JSON: %v (output: %q)", err, buf.String())
	}
	if rec["msg"] != "hello" || rec["check"] != "acme" {
		t.Errorf("unexpected fields: %v", rec)
	}
}

func TestNewLogHandlerText(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(&buf, "", "info"))
	logger.Info("hello")

	// Text handler output is not JSON.
	var rec map[string]any
	if json.Unmarshal(buf.Bytes(), &rec) == nil {
		t.Errorf("text format unexpectedly produced JSON: %q", buf.String())
	}
}

func TestNewLogHandlerLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newLogHandler(&buf, "json", "warn"))
	logger.Info("suppressed")
	logger.Warn("kept")

	if bytes.Contains(buf.Bytes(), []byte("suppressed")) {
		t.Error("info line should be filtered out at warn level")
	}
	if !bytes.Contains(buf.Bytes(), []byte("kept")) {
		t.Error("warn line should be present at warn level")
	}
}
