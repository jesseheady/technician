package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	Service    string           `yaml:"service"`
	Hostname   string           `yaml:"hostname"`
	Origins    []Origin         `yaml:"origins"`
	Metrics    MetricsConfig    `yaml:"metrics"`
	Artifacts  ArtifactsConfig  `yaml:"artifacts"`
	Playwright PlaywrightConfig `yaml:"playwright"`
	Webhooks   []WebhookConfig  `yaml:"webhooks"`
}

type WebhookConfig struct {
	URL        string        `yaml:"url"`
	Type       string        `yaml:"type"`       // "discord", "slack", "generic"
	Events     []string      `yaml:"events"`     // "check_down", "check_up", "budget_violation", "cert_expiring"
	Severities []string      `yaml:"severities"` // "warning", "critical" (empty = all)
	Cooldown   time.Duration `yaml:"cooldown"`   // minimum interval between repeated notifications; default 5m
}

type Origin struct {
	ID       string            `yaml:"id"`
	City     string            `yaml:"city"`
	Country  string            `yaml:"country"`
	Platform string            `yaml:"platform"`
	Labels   map[string]string `yaml:"labels"`
}

type MetricsConfig struct {
	Prometheus PrometheusConfig `yaml:"prometheus"`
	OTEL       OTELConfig       `yaml:"otel"`
}

type PrometheusConfig struct {
	Listen string `yaml:"listen"`
}

type OTELConfig struct {
	Endpoint string `yaml:"endpoint"`
}

type ArtifactsConfig struct {
	Driver    string `yaml:"driver"`
	Bucket    string `yaml:"bucket"`
	Region    string `yaml:"region"`
	Retention string `yaml:"retention"`
	Path      string `yaml:"path"`
}

type PlaywrightConfig struct {
	Mode        string `yaml:"mode"`
	ServerURL   string `yaml:"server_url"`
	MaxBrowsers int    `yaml:"max_browsers"` // max concurrent Chromium instances (0 = default 2)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if cfg.Metrics.Prometheus.Listen == "" {
		cfg.Metrics.Prometheus.Listen = ":9590"
	}
	if cfg.Artifacts.Driver == "" {
		cfg.Artifacts.Driver = "none"
	}
	if cfg.Playwright.Mode == "" {
		cfg.Playwright.Mode = "local"
	}
	if cfg.Playwright.MaxBrowsers <= 0 {
		cfg.Playwright.MaxBrowsers = 2
	}

	if err := validateWebhooks(cfg.Webhooks); err != nil {
		return nil, err
	}

	return &cfg, nil
}

var validWebhookTypes = map[string]bool{"discord": true, "slack": true, "generic": true}
var validWebhookEvents = map[string]bool{"check_down": true, "check_up": true, "budget_violation": true, "cert_expiring": true}
var validWebhookSeverities = map[string]bool{"warning": true, "critical": true}

func validateWebhooks(webhooks []WebhookConfig) error {
	for i, wh := range webhooks {
		if wh.URL == "" {
			return fmt.Errorf("webhook[%d]: url is required", i)
		}
		if wh.Type != "" && !validWebhookTypes[wh.Type] {
			return fmt.Errorf("webhook[%d]: invalid type %q (must be discord, slack, or generic)", i, wh.Type)
		}
		for _, e := range wh.Events {
			if !validWebhookEvents[e] {
				return fmt.Errorf("webhook[%d]: invalid event %q (must be check_down, check_up, budget_violation, or cert_expiring)", i, e)
			}
		}
		for _, s := range wh.Severities {
			if !validWebhookSeverities[s] {
				return fmt.Errorf("webhook[%d]: invalid severity %q (must be warning or critical)", i, s)
			}
		}
	}
	return nil
}

var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return match
	})
}

func (c *Config) OriginByID(id string) *Origin {
	for i := range c.Origins {
		if c.Origins[i].ID == id {
			return &c.Origins[i]
		}
	}
	return nil
}

// ResolveOrigin finds an origin by ID, falling back to the first configured origin.
func (c *Config) ResolveOrigin(id string) *Origin {
	if id != "" {
		if o := c.OriginByID(id); o != nil {
			return o
		}
	}
	if len(c.Origins) > 0 {
		return &c.Origins[0]
	}
	return nil
}

// MetricLabels returns the base Prometheus labels for this origin, merged with
// any user-defined labels from config. User labels do not override built-in keys.
func (o Origin) MetricLabels() map[string]string {
	m := map[string]string{
		"region":  o.ID,
		"city":    o.City,
		"country": o.Country,
	}
	for k, v := range o.Labels {
		if _, builtin := m[k]; !builtin {
			m[k] = v
		}
	}
	return m
}

// ResolveChecksPath returns the path to check definitions. It looks for a
// checks.yml file first, then falls back to a checks/ directory. LoadChecks
// handles both files and directories.
func ResolveChecksPath(configPath string) string {
	dir := filepath.Dir(configPath)
	// Prefer single file
	filePath := filepath.Join(dir, "checks.yml")
	if _, err := os.Stat(filePath); err == nil {
		return filePath
	}
	// Fall back to directory
	return filepath.Join(dir, "checks")
}
