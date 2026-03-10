package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHTTPProbes(t *testing.T) {
	content := `
- name: Test Site
  url: https://example.com
  expected_status: 200
  schedule: "*/30 * * * * *"
  timeout: 5s
  headers:
    X-Custom: value
- name: Test API
  url: https://api.example.com/health
  expected_status: 204
  method: HEAD
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(probes) != 2 {
		t.Fatalf("expected 2 probes, got %d", len(probes))
	}

	p := probes[0]
	if p.Name != "Test Site" {
		t.Errorf("expected name=Test Site, got %s", p.Name)
	}
	if p.Type != ProbeTypeHTTP {
		t.Errorf("expected type=http, got %s", p.Type)
	}
	if p.HTTP.URL != "https://example.com" {
		t.Errorf("expected url=https://example.com, got %s", p.HTTP.URL)
	}
	if p.HTTP.ExpectedStatus != 200 {
		t.Errorf("expected status=200, got %d", p.HTTP.ExpectedStatus)
	}
	if p.HTTP.Headers["X-Custom"] != "value" {
		t.Errorf("expected X-Custom header=value, got %s", p.HTTP.Headers["X-Custom"])
	}

	p2 := probes[1]
	if p2.HTTP.Method != "HEAD" {
		t.Errorf("expected method=HEAD, got %s", p2.HTTP.Method)
	}
	if p2.HTTP.ExpectedStatus != 204 {
		t.Errorf("expected status=204, got %d", p2.HTTP.ExpectedStatus)
	}
}

func TestLoadProbesNoDir(t *testing.T) {
	probes, err := LoadProbes("/nonexistent/path")
	if err != nil {
		t.Fatal("expected no error for missing probes dir")
	}
	if len(probes) != 0 {
		t.Errorf("expected 0 probes, got %d", len(probes))
	}
}

func TestFindProbeByName(t *testing.T) {
	probes := []ProbeConfig{
		{Name: "first"},
		{Name: "second"},
		{Name: "third"},
	}

	p := FindProbeByName(probes, "second")
	if p == nil {
		t.Fatal("expected to find probe 'second'")
	}
	if p.Name != "second" {
		t.Errorf("expected name=second, got %s", p.Name)
	}

	if FindProbeByName(probes, "missing") != nil {
		t.Error("expected nil for missing probe")
	}
}

func TestLoadHTTPProbesEnvExpansion(t *testing.T) {
	os.Setenv("TEST_API_TOKEN", "bearer-xyz")
	defer os.Unsetenv("TEST_API_TOKEN")

	content := `
- name: Authed API
  url: https://api.example.com
  expected_status: 200
  headers:
    Authorization: "Bearer ${TEST_API_TOKEN}"
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if probes[0].HTTP.Headers["Authorization"] != "Bearer bearer-xyz" {
		t.Errorf("expected env expansion, got %s", probes[0].HTTP.Headers["Authorization"])
	}
}
