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
sites:
  - code: us-east-1
    city: N. Virginia
    country: US
    geohash: dqcjq
    provider: aws
  - code: us-west-2
    city: Oregon
    country: US
    geohash: c20y
    provider: aws
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
	if len(cfg.Sites) != 2 {
		t.Errorf("expected 2 sites, got %d", len(cfg.Sites))
	}
	if cfg.Sites[0].Code != "us-east-1" {
		t.Errorf("expected first site code=us-east-1, got %s", cfg.Sites[0].Code)
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

	if cfg.Metrics.Prometheus.Listen != ":9394" {
		t.Errorf("expected default listen=:9394, got %s", cfg.Metrics.Prometheus.Listen)
	}
	if cfg.Artifacts.Driver != "none" {
		t.Errorf("expected default driver=none, got %s", cfg.Artifacts.Driver)
	}
	if cfg.Playwright.Mode != "local" {
		t.Errorf("expected default playwright mode=local, got %s", cfg.Playwright.Mode)
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

func TestSiteByCode(t *testing.T) {
	cfg := &Config{
		Sites: []Site{
			{Code: "us-east-1", City: "N. Virginia"},
			{Code: "us-west-2", City: "Oregon"},
		},
	}

	site := cfg.SiteByCode("us-west-2")
	if site == nil {
		t.Fatal("expected to find site us-west-2")
	}
	if site.City != "Oregon" {
		t.Errorf("expected city=Oregon, got %s", site.City)
	}

	if cfg.SiteByCode("unknown") != nil {
		t.Error("expected nil for unknown site code")
	}
}

func TestSiteLabels(t *testing.T) {
	site := Site{Code: "us-east-1", City: "N. Virginia", Country: "US"}
	labels := site.Labels()

	if labels["site_code"] != "us-east-1" {
		t.Errorf("expected site_code=us-east-1, got %s", labels["site_code"])
	}
	if labels["site_city"] != "N. Virginia" {
		t.Errorf("expected site_city=N. Virginia, got %s", labels["site_city"])
	}
	if labels["site_country"] != "US" {
		t.Errorf("expected site_country=US, got %s", labels["site_country"])
	}
}
