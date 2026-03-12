package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var validAssertionTypes = map[string]bool{
	"contains": true, "not_contains": true, "regex": true,
	"header_contains": true, "header_not_contains": true, "header_regex": true,
}

type ProbeType string

const (
	ProbeTypeHTTP        ProbeType = "http"
	ProbeTypeSMTP        ProbeType = "smtp"
	ProbeTypeTraceroute  ProbeType = "traceroute"
	ProbeTypePlaywright  ProbeType = "playwright"
	ProbeTypeTCP         ProbeType = "tcp"
	ProbeTypeDNS         ProbeType = "dns"
	ProbeTypeICMP        ProbeType = "icmp"
	ProbeTypeGRPC        ProbeType = "grpc"
)

type RetryPolicy struct {
	Count   int           `yaml:"count"`   // number of retries (0 = no retry)
	Backoff string        `yaml:"backoff"` // "none", "linear", "exponential"
	Delay   time.Duration `yaml:"delay"`   // base delay between retries
}

type ProbeConfig struct {
	Name     string            `yaml:"name"`
	Type     ProbeType         `yaml:"-"`
	Group    string            `yaml:"group"`
	Schedule string            `yaml:"schedule"`
	Timeout  time.Duration     `yaml:"timeout"`
	Retry    *RetryPolicy      `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"` // response time threshold for "degraded" state
	HTTP     *HTTPProbeConfig  `yaml:"-"`
	SMTP     *SMTPProbeConfig  `yaml:"-"`
	Traceroute *TracerouteProbeConfig `yaml:"-"`
	Playwright *PlaywrightProbeConfig `yaml:"-"`
	TCP      *TCPProbeConfig   `yaml:"-"`
	DNS      *DNSProbeConfig   `yaml:"-"`
	ICMP     *ICMPProbeConfig  `yaml:"-"`
	GRPC     *GRPCProbeConfig  `yaml:"-"`
}

type Assertion struct {
	Type   string `yaml:"type"`   // "contains", "not_contains", "regex", "header_contains", "header_not_contains", "header_regex"
	Header string `yaml:"header"` // header name (for header_* types)
	Target string `yaml:"target"` // string to match or regex pattern
}

type HTTPProbeConfig struct {
	URL             string            `yaml:"url"`
	Method          string            `yaml:"method"`
	ExpectedStatus  int               `yaml:"expected_status"`
	Headers         map[string]string `yaml:"headers"`
	Body            string            `yaml:"body"`
	SkipTLS         bool              `yaml:"skip_tls"`
	FollowRedirects bool             `yaml:"follow_redirects"`
	Assertions      []Assertion       `yaml:"assertions"`
}

type TCPProbeConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	IPVersion  string `yaml:"ip_version"` // "4", "6", or "" (any)
	TLS        bool   `yaml:"tls"`
	Send       string `yaml:"send"`       // optional bytes to send
	ExpectRecv string `yaml:"expect_recv"` // optional expected response substring
}

type DNSProbeConfig struct {
	Domain     string   `yaml:"domain"`
	Server     string   `yaml:"server"`      // DNS server address (e.g. "8.8.8.8:53")
	RecordType string   `yaml:"record_type"` // A, AAAA, MX, TXT, CNAME, NS, SOA, SRV
	Expected   []string `yaml:"expected"`    // expected values in answer section
}

type ICMPProbeConfig struct {
	Host      string `yaml:"host"`
	Count     int    `yaml:"count"`      // number of pings (default 3)
	IPVersion string `yaml:"ip_version"` // "4", "6", or "" (any)
}

type GRPCProbeConfig struct {
	Host    string `yaml:"host"`    // host:port
	Service string `yaml:"service"` // gRPC health check service name (empty = overall)
	TLS     bool   `yaml:"tls"`
	SkipTLS bool   `yaml:"skip_tls"` // skip TLS cert verification
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
	Name            string            `yaml:"name"`
	Group           string            `yaml:"group"`
	URL             string            `yaml:"url"`
	Method          string            `yaml:"method"`
	ExpectedStatus  int               `yaml:"expected_status"`
	Schedule        string            `yaml:"schedule"`
	Timeout         time.Duration     `yaml:"timeout"`
	Retry           *RetryPolicy      `yaml:"retry"`
	DegradedAfter   time.Duration     `yaml:"degraded_after"`
	Headers         map[string]string `yaml:"headers"`
	Body            string            `yaml:"body"`
	SkipTLS         bool              `yaml:"skip_tls"`
	FollowRedirects bool             `yaml:"follow_redirects"`
	Assertions      []Assertion       `yaml:"assertions"`
}

type smtpProbeYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Host          string        `yaml:"host"`
	Port          int           `yaml:"port"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type tracerouteProbeYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Host          string        `yaml:"host"`
	MaxHops       int           `yaml:"max_hops"`
	Count         int           `yaml:"count"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
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
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type tcpProbeYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Host          string        `yaml:"host"`
	Port          int           `yaml:"port"`
	IPVersion     string        `yaml:"ip_version"`
	TLS           bool          `yaml:"tls"`
	Send          string        `yaml:"send"`
	ExpectRecv    string        `yaml:"expect_recv"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type dnsProbeYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Domain        string        `yaml:"domain"`
	Server        string        `yaml:"server"`
	RecordType    string        `yaml:"record_type"`
	Expected      []string      `yaml:"expected"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type icmpProbeYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Host          string        `yaml:"host"`
	Count         int           `yaml:"count"`
	IPVersion     string        `yaml:"ip_version"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type grpcProbeYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Host          string        `yaml:"host"`
	Service       string        `yaml:"service"`
	TLS           bool          `yaml:"tls"`
	SkipTLS       bool          `yaml:"skip_tls"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
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

	tcpProbes, err := loadTCPProbes(filepath.Join(probesDir, "tcp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading TCP probes: %w", err)
	}
	probes = append(probes, tcpProbes...)

	dnsProbes, err := loadDNSProbes(filepath.Join(probesDir, "dns.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading DNS probes: %w", err)
	}
	probes = append(probes, dnsProbes...)

	icmpProbes, err := loadICMPProbes(filepath.Join(probesDir, "icmp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading ICMP probes: %w", err)
	}
	probes = append(probes, icmpProbes...)

	grpcProbes, err := loadGRPCProbes(filepath.Join(probesDir, "grpc.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading gRPC probes: %w", err)
	}
	probes = append(probes, grpcProbes...)

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
			Name:          r.Name,
			Type:          ProbeTypeHTTP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			HTTP: &HTTPProbeConfig{
				URL:             r.URL,
				Method:          r.Method,
				ExpectedStatus:  r.ExpectedStatus,
				Headers:         r.Headers,
				Body:            r.Body,
				SkipTLS:         r.SkipTLS,
				FollowRedirects: r.FollowRedirects,
				Assertions:      r.Assertions,
			},
		}
	}
	for i, p := range probes {
		for j, a := range p.HTTP.Assertions {
			if !validAssertionTypes[a.Type] {
				return nil, fmt.Errorf("probe %q assertion[%d]: invalid type %q", probes[i].Name, j, a.Type)
			}
			if a.Target == "" {
				return nil, fmt.Errorf("probe %q assertion[%d]: target is required", probes[i].Name, j)
			}
			if strings.HasPrefix(a.Type, "header_") && a.Header == "" {
				return nil, fmt.Errorf("probe %q assertion[%d]: header is required for type %q", probes[i].Name, j, a.Type)
			}
			if a.Type == "regex" || a.Type == "header_regex" {
				if _, err := regexp.Compile(a.Target); err != nil {
					return nil, fmt.Errorf("probe %q assertion[%d]: invalid regex %q: %w", probes[i].Name, j, a.Target, err)
				}
			}
		}
	}

	return probes, nil
}

func loadSMTPProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []smtpProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		if r.Port == 0 {
			r.Port = 25
		}
		probes[i] = ProbeConfig{
			Name:          r.Name,
			Type:          ProbeTypeSMTP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
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

	expanded := expandEnvVars(string(data))

	var raw []tracerouteProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
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
			Name:          r.Name,
			Type:          ProbeTypeTraceroute,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
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
			Name:          r.Name,
			Type:          ProbeTypePlaywright,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
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

func loadTCPProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []tcpProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		probes[i] = ProbeConfig{
			Name:          r.Name,
			Type:          ProbeTypeTCP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			TCP: &TCPProbeConfig{
				Host:       r.Host,
				Port:       r.Port,
				IPVersion:  r.IPVersion,
				TLS:        r.TLS,
				Send:       r.Send,
				ExpectRecv: r.ExpectRecv,
			},
		}
	}
	return probes, nil
}

func loadDNSProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []dnsProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		if r.RecordType == "" {
			r.RecordType = "A"
		}
		if r.Server == "" {
			r.Server = "8.8.8.8:53"
		}
		probes[i] = ProbeConfig{
			Name:          r.Name,
			Type:          ProbeTypeDNS,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			DNS: &DNSProbeConfig{
				Domain:     r.Domain,
				Server:     r.Server,
				RecordType: r.RecordType,
				Expected:   r.Expected,
			},
		}
	}
	return probes, nil
}

func loadICMPProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []icmpProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		if r.Count == 0 {
			r.Count = 3
		}
		probes[i] = ProbeConfig{
			Name:          r.Name,
			Type:          ProbeTypeICMP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			ICMP: &ICMPProbeConfig{
				Host:      r.Host,
				Count:     r.Count,
				IPVersion: r.IPVersion,
			},
		}
	}
	return probes, nil
}

func loadGRPCProbes(path string) ([]ProbeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []grpcProbeYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	probes := make([]ProbeConfig, len(raw))
	for i, r := range raw {
		probes[i] = ProbeConfig{
			Name:          r.Name,
			Type:          ProbeTypeGRPC,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			GRPC: &GRPCProbeConfig{
				Host:    r.Host,
				Service: r.Service,
				TLS:     r.TLS,
				SkipTLS: r.SkipTLS,
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
