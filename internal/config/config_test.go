package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	content := `
service: technician
hostname: test.example.com
origins:
  - id: us-east-1
    city: N. Virginia
    country: US
    # location_hash removed dqcjq
    platform: aws
  - id: us-west-2
    city: Oregon
    country: US
    # location_hash removed c20y
    platform: aws
metrics:
  prometheus:
    listen: ":9999"
artifacts:
  driver: local
  path: /tmp/test
`
	dir := t.TempDir()
	path := filepath.Join(dir, "technician.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Service != "technician" {
		t.Errorf("expected service=technician, got %s", cfg.Service)
	}
	if len(cfg.Origins) != 2 {
		t.Errorf("expected 2 sites, got %d", len(cfg.Origins))
	}
	if cfg.Origins[0].ID != "us-east-1" {
		t.Errorf("expected first site code=us-east-1, got %s", cfg.Origins[0].ID)
	}
	if cfg.Metrics.Prometheus.Listen != ":9999" {
		t.Errorf("expected listen=:9999, got %s", cfg.Metrics.Prometheus.Listen)
	}
	if cfg.Artifacts.Driver != "local" {
		t.Errorf("expected driver=local, got %s", cfg.Artifacts.Driver)
	}
}

func TestLoadDefaults(t *testing.T) {
	content := `service: test`
	dir := t.TempDir()
	path := filepath.Join(dir, "technician.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Metrics.Prometheus.Listen != ":9590" {
		t.Errorf("expected default listen=:9590, got %s", cfg.Metrics.Prometheus.Listen)
	}
	if cfg.Artifacts.Driver != "none" {
		t.Errorf("expected default driver=none, got %s", cfg.Artifacts.Driver)
	}
	if cfg.Playwright.Mode != "local" {
		t.Errorf("expected default playwright mode=local, got %s", cfg.Playwright.Mode)
	}
	if cfg.Playwright.MaxBrowsers != 2 {
		t.Errorf("expected default max_browsers=2, got %d", cfg.Playwright.MaxBrowsers)
	}
	if cfg.Metrics.Prometheus.MaxCheckCardinality != DefaultMaxCheckCardinality {
		t.Errorf("expected default max_check_cardinality=%d, got %d",
			DefaultMaxCheckCardinality, cfg.Metrics.Prometheus.MaxCheckCardinality)
	}
}

func TestLoadMaxCheckCardinality(t *testing.T) {
	content := `
service: test
metrics:
  prometheus:
    max_check_cardinality: 2000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "technician.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Metrics.Prometheus.MaxCheckCardinality != 2000 {
		t.Errorf("expected max_check_cardinality=2000, got %d", cfg.Metrics.Prometheus.MaxCheckCardinality)
	}
}

func TestLoadPlaywrightMaxBrowsers(t *testing.T) {
	content := `
service: test
playwright:
  max_browsers: 4
`
	dir := t.TempDir()
	path := filepath.Join(dir, "technician.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Playwright.MaxBrowsers != 4 {
		t.Errorf("expected max_browsers=4, got %d", cfg.Playwright.MaxBrowsers)
	}
}

func TestExpandEnvVars(t *testing.T) {
	os.Setenv("TEST_TOKEN", "secret123")
	defer os.Unsetenv("TEST_TOKEN")

	input := "Authorization: Bearer ${TEST_TOKEN}"
	result := expandEnvVars(input)

	expected := "Authorization: Bearer secret123"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExpandEnvVarsUnset(t *testing.T) {
	os.Unsetenv("UNSET_VAR")

	input := "value: ${UNSET_VAR}"
	result := expandEnvVars(input)

	if result != input {
		t.Errorf("expected unset var to remain, got %q", result)
	}
}

func TestOriginByID(t *testing.T) {
	cfg := &Config{
		Origins: []Origin{
			{ID: "us-east-1", City: "N. Virginia"},
			{ID: "us-west-2", City: "Oregon"},
		},
	}

	origin := cfg.OriginByID("us-west-2")
	if origin == nil {
		t.Fatal("expected to find site us-west-2")
	}
	if origin.City != "Oregon" {
		t.Errorf("expected city=Oregon, got %s", origin.City)
	}

	if cfg.OriginByID("unknown") != nil {
		t.Error("expected nil for unknown site code")
	}
}

func TestSiteLabels(t *testing.T) {
	origin := Origin{ID: "us-east-1", City: "N. Virginia", Country: "US"}
	labels := origin.MetricLabels()

	if labels["region"] != "us-east-1" {
		t.Errorf("expected region=us-east-1, got %s", labels["region"])
	}
	if labels["city"] != "N. Virginia" {
		t.Errorf("expected city=N. Virginia, got %s", labels["city"])
	}
	if labels["country"] != "US" {
		t.Errorf("expected country=US, got %s", labels["country"])
	}
}

func TestPlaywrightModeValidation(t *testing.T) {
	base := `
service: technician
origins:
  - id: local
    platform: docker
`
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"unset defaults to local", "", false},
		{"local needs no server_url", "playwright:\n  mode: local\n", false},
		{"managed with server_url", "playwright:\n  mode: managed\n  server_url: ws://pw:3000/\n", false},
		{"managed without server_url", "playwright:\n  mode: managed\n", true},
		{"unknown mode", "playwright:\n  mode: remote\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "technician.yml")
			if err := os.WriteFile(path, []byte(base+tt.pw), 0o644); err != nil {
				t.Fatalf("writing config: %v", err)
			}

			_, err := Load(path)
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPlaywrightModeDefaultsToLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "technician.yml")
	content := "service: technician\norigins:\n  - id: local\n    platform: docker\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Playwright.Mode != "local" {
		t.Errorf("Playwright.Mode = %q, want %q", cfg.Playwright.Mode, "local")
	}
}

func TestExpandEnvVarsDefaults(t *testing.T) {
	t.Setenv("TECH_SET", "value")
	tests := []struct{ in, want string }{
		{"${TECH_SET}", "value"},
		{"${TECH_SET:-fallback}", "value"},
		{"${TECH_UNSET:-fallback}", "fallback"},
		{"${TECH_UNSET:-}", ""},
		{"${TECH_UNSET}", "${TECH_UNSET}"}, // no default: placeholder kept, as before
		{"ws://${TECH_UNSET:-host}:3000/", "ws://host:3000/"},
	}
	for _, tt := range tests {
		if got := expandEnvVars(tt.in); got != tt.want {
			t.Errorf("expandEnvVars(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
