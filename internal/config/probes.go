package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type ProbeType string

const (
	ProbeTypeHTTP        ProbeType = "http"
	ProbeTypeSMTP        ProbeType = "smtp"
	ProbeTypeTraceroute  ProbeType = "traceroute"
	ProbeTypePlaywright  ProbeType = "playwright"
)

type ProbeConfig struct {
	Name     string            `yaml:"name"`
	Type     ProbeType         `yaml:"-"`
	Group    string            `yaml:"group"`
	Schedule string            `yaml:"schedule"`
	Timeout  time.Duration     `yaml:"timeout"`
	HTTP     *HTTPProbeConfig  `yaml:"-"`
	SMTP     *SMTPProbeConfig  `yaml:"-"`
	Traceroute *TracerouteProbeConfig `yaml:"-"`
	Playwright *PlaywrightProbeConfig `yaml:"-"`
}

type HTTPProbeConfig struct {
	URL            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	ExpectedStatus int               `yaml:"expected_status"`
	Headers        map[string]string `yaml:"headers"`
	Body           string            `yaml:"body"`
	SkipTLS        bool              `yaml:"skip_tls"`
}

type SMTPProbeConfig struct {
	Host    string        `yaml:"host"`
	Port    int           `yaml:"port"`
	Timeout time.Duration `yaml:"timeout"`
}

type TracerouteProbeConfig struct {
	Host     string `yaml:"host"`
	MaxHops  int    `yaml:"max_hops"`
	Count    int    `yaml:"count"`
}

type PlaywrightProbeConfig struct {
	Script        string `yaml:"script"`
	Authenticator string `yaml:"authenticator"`
	BaseURL       string `yaml:"base_url"`
	Video         bool   `yaml:"video"`
	Network       string `yaml:"network"`  // 4g, 3g, slow-3g, or empty (no throttling)
	Device        string `yaml:"device"`   // Playwright device name, e.g. "iPhone 14", "Pixel 7"
}

type httpProbeYAML struct {
	Name           string            `yaml:"name"`
	Group          string            `yaml:"group"`
	URL            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	ExpectedStatus int               `yaml:"expected_status"`
	Schedule       string            `yaml:"schedule"`
	Timeout        time.Duration     `yaml:"timeout"`
	Headers        map[string]string `yaml:"headers"`
	Body           string            `yaml:"body"`
	SkipTLS        bool              `yaml:"skip_tls"`
}

type smtpProbeYAML struct {
	Name     string        `yaml:"name"`
	Group    string        `yaml:"group"`
	Host     string        `yaml:"host"`
	Port     int           `yaml:"port"`
	Schedule string        `yaml:"schedule"`
	Timeout  time.Duration `yaml:"timeout"`
}

type tracerouteProbeYAML struct {
	Name     string        `yaml:"name"`
	Group    string        `yaml:"group"`
	Host     string        `yaml:"host"`
	MaxHops  int           `yaml:"max_hops"`
	Count    int           `yaml:"count"`
	Schedule string        `yaml:"schedule"`
	Timeout  time.Duration `yaml:"timeout"`
}

type playwrightProbeYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Script        string        `yaml:"script"`
	Authenticator string        `yaml:"authenticator"`
	BaseURL       string        `yaml:"base_url"`
	Video         bool          `yaml:"video"`
	Network       string        `yaml:"network"`
	Device        string        `yaml:"device"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
}

func LoadProbes(probesDir string) ([]ProbeConfig, error) {
	var probes []ProbeConfig

	httpProbes, err := loadHTTPProbes(filepath.Join(probesDir, "http.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading HTTP probes: %w", err)
	}
	probes = append(probes, httpProbes...)

	smtpProbes, err := loadSMTPProbes(filepath.Join(probesDir, "smtp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading SMTP probes: %w", err)
	}
	probes = append(probes, smtpProbes...)

	trProbes, err := loadTracerouteProbes(filepath.Join(probesDir, "traceroute.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading traceroute probes: %w", err)
	}
	probes = append(probes, trProbes...)

	pwProbes, err := loadPlaywrightProbes(probesDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading Playwright probes: %w", err)
	}
	probes = append(probes, pwProbes...)

	for i := range probes {
		if probes[i].Timeout == 0 {
			probes[i].Timeout = 30 * time.Second
		}
		if probes[i].Schedule == "" {
			probes[i].Schedule = "*/30 * * * * *"
		}
	}

	return probes, nil
}

func loadHTTPProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []httpProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		if r.ExpectedStatus == 0 {
			r.ExpectedStatus = 200
		}
		if r.Method == "" {
			r.Method = "GET"
		}
		probes[i] = ProbeConfig{
			Name:     r.Name,
			Type:     ProbeTypeHTTP,
			Group:    r.Group,
			Schedule: r.Schedule,
			Timeout:  r.Timeout,
			HTTP: &HTTPProbeConfig{
				URL:            r.URL,
				Method:         r.Method,
				ExpectedStatus: r.ExpectedStatus,
				Headers:        r.Headers,
				Body:           r.Body,
				SkipTLS:        r.SkipTLS,
			},
		}
	}
	return probes, nil
}

func loadSMTPProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw []smtpProbeYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		if r.Port == 0 {
			r.Port = 25
		}
		probes[i] = ProbeConfig{
			Name:     r.Name,
			Type:     ProbeTypeSMTP,
			Group:    r.Group,
			Schedule: r.Schedule,
			Timeout:  r.Timeout,
			SMTP: &SMTPProbeConfig{
				Host:    r.Host,
				Port:    r.Port,
				Timeout: r.Timeout,
			},
		}
	}
	return probes, nil
}

func loadTracerouteProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw []tracerouteProbeYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		if r.MaxHops == 0 {
			r.MaxHops = 30
		}
		if r.Count == 0 {
			r.Count = 3
		}
		probes[i] = ProbeConfig{
			Name:     r.Name,
			Type:     ProbeTypeTraceroute,
			Group:    r.Group,
			Schedule: r.Schedule,
			Timeout:  r.Timeout,
			Traceroute: &TracerouteProbeConfig{
				Host:    r.Host,
				MaxHops: r.MaxHops,
				Count:   r.Count,
			},
		}
	}
	return probes, nil
}

func loadPlaywrightProbes(probesDir string) ([]ProbeConfig, error) {
	pwDir := filepath.Join(probesDir, "playwright")
	configPath := filepath.Join(pwDir, "playwright.yml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Also try probes/playwright.yml at parent level
		data, err = os.ReadFile(filepath.Join(probesDir, "playwright.yml"))
		if err != nil {
			return nil, err
		}
	}

	expanded := expandEnvVars(string(data))

	var raw []playwrightProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing playwright config: %w", err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		// Resolve script path relative to probes directory
		script := r.Script
		if !filepath.IsAbs(script) {
			script = filepath.Join(probesDir, script)
		}
		probes[i] = ProbeConfig{
			Name:     r.Name,
			Type:     ProbeTypePlaywright,
			Group:    r.Group,
			Schedule: r.Schedule,
			Timeout:  r.Timeout,
			Playwright: &PlaywrightProbeConfig{
				Script:        script,
				Authenticator: r.Authenticator,
				BaseURL:       r.BaseURL,
				Video:         r.Video,
				Network:       r.Network,
				Device:        r.Device,
			},
		}
	}
	return probes, nil
}

func FindProbeByName(probes []ProbeConfig, name string) *ProbeConfig {
	for i := range probes {
		if probes[i].Name == name {
			return &probes[i]
		}
	}
	return nil
}
