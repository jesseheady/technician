package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Service    string           `yaml:"service"`
	Hostname   string           `yaml:"hostname"`
	Sites      []Site           `yaml:"sites"`
	Metrics    MetricsConfig    `yaml:"metrics"`
	Artifacts  ArtifactsConfig  `yaml:"artifacts"`
	Playwright PlaywrightConfig `yaml:"playwright"`
}

type Site struct {
	Code     string `yaml:"code"`
	City     string `yaml:"city"`
	Country  string `yaml:"country"`
	Geohash  string `yaml:"geohash"`
	Provider string `yaml:"provider"`
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
	Mode      string `yaml:"mode"`
	ServerURL string `yaml:"server_url"`
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
		cfg.Metrics.Prometheus.Listen = ":9394"
	}
	if cfg.Artifacts.Driver == "" {
		cfg.Artifacts.Driver = "none"
	}
	if cfg.Playwright.Mode == "" {
		cfg.Playwright.Mode = "local"
	}

	return &cfg, nil
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

func (c *Config) SiteByCode(code string) *Site {
	for i := range c.Sites {
		if c.Sites[i].Code == code {
			return &c.Sites[i]
		}
	}
	return nil
}

// ResolveSite finds a site by code, falling back to the first configured site.
func (c *Config) ResolveSite(code string) *Site {
	if code != "" {
		if s := c.SiteByCode(code); s != nil {
			return s
		}
	}
	if len(c.Sites) > 0 {
		return &c.Sites[0]
	}
	return nil
}

func (s Site) Labels() map[string]string {
	return map[string]string{
		"site_code":    s.Code,
		"site_city":    s.City,
		"site_country": s.Country,
	}
}

func ResolveProbesDir(configPath string) string {
	dir := filepath.Dir(configPath)
	return filepath.Join(dir, "probes")
}
