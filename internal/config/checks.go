package config

import (
	"fmt"
	"net"
	"net/url"
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

type CheckType string

const (
	CheckTypeHTTP        CheckType = "http"
	CheckTypeSMTP        CheckType = "smtp"
	CheckTypeTraceroute  CheckType = "traceroute"
	CheckTypePlaywright  CheckType = "playwright"
	CheckTypeTCP         CheckType = "tcp"
	CheckTypeDNS         CheckType = "dns"
	CheckTypeICMP        CheckType = "icmp"
	CheckTypeGRPC        CheckType = "grpc"
	CheckTypeNTP         CheckType = "ntp"
	CheckTypeTLS         CheckType = "tls"
	CheckTypeUDP         CheckType = "udp"
	CheckTypeBGP         CheckType = "bgp"
	CheckTypeDomainExpiry CheckType = "domain_expiry"
)

type RetryPolicy struct {
	Count   int           `yaml:"count"`   // number of retries (0 = no retry)
	Backoff string        `yaml:"backoff"` // "none", "linear", "exponential"
	Delay   time.Duration `yaml:"delay"`   // base delay between retries
}

type CheckConfig struct {
	Name     string            `yaml:"name"`
	Type     CheckType         `yaml:"-"`
	Group    string            `yaml:"group"`
	Schedule string            `yaml:"schedule"`
	Timeout  time.Duration     `yaml:"timeout"`
	Retry    *RetryPolicy      `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"` // response time threshold for "degraded" state
	HTTP     *HTTPCheckConfig  `yaml:"-"`
	SMTP     *SMTPCheckConfig  `yaml:"-"`
	Traceroute *TracerouteCheckConfig `yaml:"-"`
	Playwright *PlaywrightCheckConfig `yaml:"-"`
	TCP      *TCPCheckConfig   `yaml:"-"`
	DNS      *DNSCheckConfig   `yaml:"-"`
	ICMP     *ICMPCheckConfig  `yaml:"-"`
	GRPC     *GRPCCheckConfig  `yaml:"-"`
	NTP      *NTPCheckConfig   `yaml:"-"`
	TLS      *TLSCheckConfig   `yaml:"-"`
	UDP          *UDPCheckConfig              `yaml:"-"`
	BGP          *BGPCheckConfig              `yaml:"-"`
	DomainExpiry *DomainExpirationCheckConfig `yaml:"-"`
}

// Target returns the canonical hostname or IP that this check checks.
// Used for domain-level grouping on the status page.
func (p *CheckConfig) Target() string {
	var raw string
	switch p.Type {
	case CheckTypeHTTP:
		if p.HTTP != nil {
			if u, err := url.Parse(p.HTTP.URL); err == nil {
				raw = u.Hostname()
			}
		}
	case CheckTypeTCP:
		if p.TCP != nil {
			raw = p.TCP.Host
		}
	case CheckTypeUDP:
		if p.UDP != nil {
			raw = p.UDP.Host
		}
	case CheckTypeDNS:
		if p.DNS != nil {
			raw = p.DNS.Domain
		}
	case CheckTypeICMP:
		if p.ICMP != nil {
			raw = p.ICMP.Host
		}
	case CheckTypeGRPC:
		if p.GRPC != nil {
			raw = p.GRPC.Host
		}
	case CheckTypeNTP:
		if p.NTP != nil {
			raw = p.NTP.Server
		}
	case CheckTypeTLS:
		if p.TLS != nil {
			raw = p.TLS.Host
		}
	case CheckTypeSMTP:
		if p.SMTP != nil {
			raw = p.SMTP.Host
		}
	case CheckTypeTraceroute:
		if p.Traceroute != nil {
			raw = p.Traceroute.Host
		}
	case CheckTypePlaywright:
		if p.Playwright != nil {
			if u, err := url.Parse(p.Playwright.BaseURL); err == nil {
				raw = u.Hostname()
			}
		}
	case CheckTypeBGP:
		if p.BGP != nil {
			raw = p.BGP.Prefix
		}
	case CheckTypeDomainExpiry:
		if p.DomainExpiry != nil {
			raw = p.DomainExpiry.Domain
		}
	}
	// Strip port from host:port patterns (e.g. gRPC "host:443", TLS "host:443")
	if h, _, err := net.SplitHostPort(raw); err == nil {
		return h
	}
	return raw
}

type Assertion struct {
	Type   string `yaml:"type"`   // "contains", "not_contains", "regex", "header_contains", "header_not_contains", "header_regex"
	Header string `yaml:"header"` // header name (for header_* types)
	Target string `yaml:"target"` // string to match or regex pattern
}

type HTTPCheckConfig struct {
	URL             string            `yaml:"url"`
	Method          string            `yaml:"method"`
	ExpectedStatus  int               `yaml:"expected_status"`
	Headers         map[string]string `yaml:"headers"`
	Body            string            `yaml:"body"`
	SkipTLS         bool              `yaml:"skip_tls"`
	FollowRedirects bool             `yaml:"follow_redirects"`
	Assertions      []Assertion       `yaml:"assertions"`
}

type TCPCheckConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	IPVersion  string `yaml:"ip_version"` // "4", "6", or "" (any)
	TLS        bool   `yaml:"tls"`
	Send       string `yaml:"send"`       // optional bytes to send
	ExpectRecv string `yaml:"expect_recv"` // optional expected response substring
}

type DNSCheckConfig struct {
	Domain     string   `yaml:"domain"`
	Server     string   `yaml:"server"`      // DNS server address (e.g. "8.8.8.8:53")
	RecordType string   `yaml:"record_type"` // A, AAAA, MX, TXT, CNAME, NS, SOA, SRV
	Expected   []string `yaml:"expected"`    // expected values in answer section
}

type ICMPCheckConfig struct {
	Host      string `yaml:"host"`
	Count     int    `yaml:"count"`      // number of pings (default 3)
	IPVersion string `yaml:"ip_version"` // "4", "6", or "" (any)
}

type GRPCCheckConfig struct {
	Host    string `yaml:"host"`    // host:port
	Service string `yaml:"service"` // gRPC health check service name (empty = overall)
	TLS     bool   `yaml:"tls"`
	SkipTLS bool   `yaml:"skip_tls"` // skip TLS cert verification
}

type NTPCheckConfig struct {
	Server string `yaml:"server"` // NTP server hostname or IP (e.g. "pool.ntp.org")
	Port   int    `yaml:"port"`   // UDP port (default 123)
}

type TLSCheckConfig struct {
	Host         string `yaml:"host"`          // host:port (e.g. "api.example.com:443")
	CheckExpiry  bool   `yaml:"check_expiry"`  // check certificate expiry (default true)
	WarnDays     int    `yaml:"warn_days"`     // days before expiry to warn (default 30)
	CriticalDays int    `yaml:"critical_days"` // days before expiry to critical (default 7)
}

type UDPCheckConfig struct {
	Host             string `yaml:"host"`
	Port             int    `yaml:"port"`
	IPVersion        string `yaml:"ip_version"`      // "4", "6", or "" (any)
	Send             string `yaml:"send"`             // payload to send (plain text)
	SendHex          string `yaml:"send_hex"`         // payload to send (hex-encoded bytes)
	ExpectResponse   *bool  `yaml:"expect_response"`  // nil defaults to true
	ExpectRecv       string `yaml:"expect_recv"`      // expected substring in response
	MaxResponseBytes int    `yaml:"max_response_bytes"` // default 4096
}

type BGPCheckConfig struct {
	Prefix         string `yaml:"prefix"`          // IP prefix to monitor (e.g. "203.0.113.0/24")
	ExpectedOrigin int    `yaml:"expected_origin"` // expected origin ASN
}

type DomainExpirationCheckConfig struct {
	Domain       string `yaml:"domain"`
	WarnDays     int    `yaml:"warn_days"`     // days before expiry to warn (default 30)
	CriticalDays int    `yaml:"critical_days"` // days before expiry to critical (default 7)
}

type SMTPCheckConfig struct {
	Host    string        `yaml:"host"`
	Port    int           `yaml:"port"`
	Timeout time.Duration `yaml:"timeout"`
}

type TracerouteCheckConfig struct {
	Host     string `yaml:"host"`
	MaxHops  int    `yaml:"max_hops"`
	Count    int    `yaml:"count"`
}

type PlaywrightCheckConfig struct {
	Script        string `yaml:"script"`
	Authenticator string `yaml:"authenticator"`
	BaseURL       string `yaml:"base_url"`
	Video         bool   `yaml:"video"`
	Network       string `yaml:"network"`  // 4g, 3g, slow-3g, or empty (no throttling)
	Device        string `yaml:"device"`   // Playwright device name, e.g. "iPhone 14", "Pixel 7"
}

type httpCheckYAML struct {
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

type smtpCheckYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Host          string        `yaml:"host"`
	Port          int           `yaml:"port"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type tracerouteCheckYAML struct {
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

type playwrightCheckYAML struct {
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

type tcpCheckYAML struct {
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

type dnsCheckYAML struct {
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

type icmpCheckYAML struct {
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

type ntpCheckYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Server        string        `yaml:"server"`
	Port          int           `yaml:"port"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type tlsCheckYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Host          string        `yaml:"host"`
	CheckExpiry   *bool         `yaml:"check_expiry"`
	WarnDays      int           `yaml:"warn_days"`
	CriticalDays  int           `yaml:"critical_days"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`
}

type udpCheckYAML struct {
	Name             string        `yaml:"name"`
	Group            string        `yaml:"group"`
	Host             string        `yaml:"host"`
	Port             int           `yaml:"port"`
	IPVersion        string        `yaml:"ip_version"`
	Send             string        `yaml:"send"`
	SendHex          string        `yaml:"send_hex"`
	ExpectResponse   *bool         `yaml:"expect_response"`
	ExpectRecv       string        `yaml:"expect_recv"`
	MaxResponseBytes int           `yaml:"max_response_bytes"`
	Schedule         string        `yaml:"schedule"`
	Timeout          time.Duration `yaml:"timeout"`
	Retry            *RetryPolicy  `yaml:"retry"`
	DegradedAfter    time.Duration `yaml:"degraded_after"`
}

type grpcCheckYAML struct {
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

type bgpCheckYAML struct {
	Name           string        `yaml:"name"`
	Group          string        `yaml:"group"`
	Prefix         string        `yaml:"prefix"`
	ExpectedOrigin int           `yaml:"expected_origin"`
	Schedule       string        `yaml:"schedule"`
	Timeout        time.Duration `yaml:"timeout"`
	Retry          *RetryPolicy  `yaml:"retry"`
	DegradedAfter  time.Duration `yaml:"degraded_after"`
}

type domainExpiryCheckYAML struct {
	Name          string        `yaml:"name"`
	Group         string        `yaml:"group"`
	Domain        string        `yaml:"domain"`
	WarnDays      int           `yaml:"warn_days"`
	CriticalDays  int           `yaml:"critical_days"`
	Schedule      string        `yaml:"schedule"`
	Timeout        time.Duration `yaml:"timeout"`
	Retry          *RetryPolicy  `yaml:"retry"`
	DegradedAfter  time.Duration `yaml:"degraded_after"`
}

func LoadChecks(checksDir string) ([]CheckConfig, error) {
	var checks []CheckConfig

	httpChecks, err := loadHTTPChecks(filepath.Join(checksDir, "http.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading HTTP checks: %w", err)
	}
	checks = append(checks, httpChecks...)

	smtpChecks, err := loadSMTPChecks(filepath.Join(checksDir, "smtp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading SMTP checks: %w", err)
	}
	checks = append(checks, smtpChecks...)

	trChecks, err := loadTracerouteChecks(filepath.Join(checksDir, "traceroute.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading traceroute checks: %w", err)
	}
	checks = append(checks, trChecks...)

	pwChecks, err := loadPlaywrightChecks(checksDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading Playwright checks: %w", err)
	}
	checks = append(checks, pwChecks...)

	tcpChecks, err := loadTCPChecks(filepath.Join(checksDir, "tcp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading TCP checks: %w", err)
	}
	checks = append(checks, tcpChecks...)

	dnsChecks, err := loadDNSChecks(filepath.Join(checksDir, "dns.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading DNS checks: %w", err)
	}
	checks = append(checks, dnsChecks...)

	icmpChecks, err := loadICMPChecks(filepath.Join(checksDir, "icmp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading ICMP checks: %w", err)
	}
	checks = append(checks, icmpChecks...)

	grpcChecks, err := loadGRPCChecks(filepath.Join(checksDir, "grpc.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading gRPC checks: %w", err)
	}
	checks = append(checks, grpcChecks...)

	ntpChecks, err := loadNTPChecks(filepath.Join(checksDir, "ntp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading NTP checks: %w", err)
	}
	checks = append(checks, ntpChecks...)

	udpChecks, err := loadUDPChecks(filepath.Join(checksDir, "udp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading UDP checks: %w", err)
	}
	checks = append(checks, udpChecks...)

	tlsChecks, err := loadTLSChecks(filepath.Join(checksDir, "tls.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading TLS checks: %w", err)
	}
	checks = append(checks, tlsChecks...)

	bgpChecks, err := loadBGPChecks(filepath.Join(checksDir, "bgp.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading BGP checks: %w", err)
	}
	checks = append(checks, bgpChecks...)

	domainExpiryChecks, err := loadDomainExpiryChecks(filepath.Join(checksDir, "domain_expiry.yml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading domain expiry checks: %w", err)
	}
	checks = append(checks, domainExpiryChecks...)

	for i := range checks {
		if checks[i].Timeout == 0 {
			checks[i].Timeout = 30 * time.Second
		}
		if checks[i].Schedule == "" {
			checks[i].Schedule = "*/30 * * * * *"
		}
	}

	return checks, nil
}

func loadHTTPChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []httpCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		if r.ExpectedStatus == 0 {
			r.ExpectedStatus = 200
		}
		if r.Method == "" {
			r.Method = "GET"
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeHTTP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			HTTP: &HTTPCheckConfig{
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
	for i, p := range checks {
		for j, a := range p.HTTP.Assertions {
			if !validAssertionTypes[a.Type] {
				return nil, fmt.Errorf("check %q assertion[%d]: invalid type %q", checks[i].Name, j, a.Type)
			}
			if a.Target == "" {
				return nil, fmt.Errorf("check %q assertion[%d]: target is required", checks[i].Name, j)
			}
			if strings.HasPrefix(a.Type, "header_") && a.Header == "" {
				return nil, fmt.Errorf("check %q assertion[%d]: header is required for type %q", checks[i].Name, j, a.Type)
			}
			if a.Type == "regex" || a.Type == "header_regex" {
				if _, err := regexp.Compile(a.Target); err != nil {
					return nil, fmt.Errorf("check %q assertion[%d]: invalid regex %q: %w", checks[i].Name, j, a.Target, err)
				}
			}
		}
	}

	return checks, nil
}

func loadSMTPChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []smtpCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		if r.Port == 0 {
			r.Port = 25
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeSMTP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			SMTP: &SMTPCheckConfig{
				Host:    r.Host,
				Port:    r.Port,
				Timeout: r.Timeout,
			},
		}
	}
	return checks, nil
}

func loadTracerouteChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []tracerouteCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		if r.MaxHops == 0 {
			r.MaxHops = 30
		}
		if r.Count == 0 {
			r.Count = 3
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeTraceroute,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			Traceroute: &TracerouteCheckConfig{
				Host:    r.Host,
				MaxHops: r.MaxHops,
				Count:   r.Count,
			},
		}
	}
	return checks, nil
}

func loadPlaywrightChecks(checksDir string) ([]CheckConfig, error) {
	pwDir := filepath.Join(checksDir, "playwright")
	configPath := filepath.Join(pwDir, "playwright.yml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Also try checks/playwright.yml at parent level
		data, err = os.ReadFile(filepath.Join(checksDir, "playwright.yml"))
		if err != nil {
			return nil, err
		}
	}

	expanded := expandEnvVars(string(data))

	var raw []playwrightCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing playwright config: %w", err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		// Resolve script path relative to checks directory
		script := r.Script
		if !filepath.IsAbs(script) {
			script = filepath.Join(checksDir, script)
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypePlaywright,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			Playwright: &PlaywrightCheckConfig{
				Script:        script,
				Authenticator: r.Authenticator,
				BaseURL:       r.BaseURL,
				Video:         r.Video,
				Network:       r.Network,
				Device:        r.Device,
			},
		}
	}
	return checks, nil
}

func loadTCPChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []tcpCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeTCP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			TCP: &TCPCheckConfig{
				Host:       r.Host,
				Port:       r.Port,
				IPVersion:  r.IPVersion,
				TLS:        r.TLS,
				Send:       r.Send,
				ExpectRecv: r.ExpectRecv,
			},
		}
	}
	return checks, nil
}

func loadDNSChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []dnsCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		if r.RecordType == "" {
			r.RecordType = "A"
		}
		if r.Server == "" {
			r.Server = "8.8.8.8:53"
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeDNS,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			DNS: &DNSCheckConfig{
				Domain:     r.Domain,
				Server:     r.Server,
				RecordType: r.RecordType,
				Expected:   r.Expected,
			},
		}
	}
	return checks, nil
}

func loadICMPChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []icmpCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		if r.Count == 0 {
			r.Count = 3
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeICMP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			ICMP: &ICMPCheckConfig{
				Host:      r.Host,
				Count:     r.Count,
				IPVersion: r.IPVersion,
			},
		}
	}
	return checks, nil
}

func loadGRPCChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []grpcCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeGRPC,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			GRPC: &GRPCCheckConfig{
				Host:    r.Host,
				Service: r.Service,
				TLS:     r.TLS,
				SkipTLS: r.SkipTLS,
			},
		}
	}
	return checks, nil
}

func loadNTPChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []ntpCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		if r.Port == 0 {
			r.Port = 123
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeNTP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			NTP: &NTPCheckConfig{
				Server: r.Server,
				Port:   r.Port,
			},
		}
	}
	return checks, nil
}

func loadTLSChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []tlsCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		checkExpiry := true
		if r.CheckExpiry != nil {
			checkExpiry = *r.CheckExpiry
		}
		warnDays := r.WarnDays
		if warnDays == 0 {
			warnDays = 30
		}
		criticalDays := r.CriticalDays
		if criticalDays == 0 {
			criticalDays = 7
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeTLS,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			TLS: &TLSCheckConfig{
				Host:         r.Host,
				CheckExpiry:  checkExpiry,
				WarnDays:     warnDays,
				CriticalDays: criticalDays,
			},
		}
	}
	return checks, nil
}

func loadUDPChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []udpCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		maxResp := r.MaxResponseBytes
		if maxResp == 0 {
			maxResp = 4096
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeUDP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			UDP: &UDPCheckConfig{
				Host:             r.Host,
				Port:             r.Port,
				IPVersion:        r.IPVersion,
				Send:             r.Send,
				SendHex:          r.SendHex,
				ExpectResponse:   r.ExpectResponse,
				ExpectRecv:       r.ExpectRecv,
				MaxResponseBytes: maxResp,
			},
		}
	}
	return checks, nil
}

func loadBGPChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []bgpCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeBGP,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			BGP: &BGPCheckConfig{
				Prefix:         r.Prefix,
				ExpectedOrigin: r.ExpectedOrigin,
			},
		}
	}
	return checks, nil
}

func loadDomainExpiryChecks(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []domainExpiryCheckYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, len(raw))
	for i, r := range raw {
		warnDays := r.WarnDays
		if warnDays == 0 {
			warnDays = 30
		}
		criticalDays := r.CriticalDays
		if criticalDays == 0 {
			criticalDays = 7
		}
		checks[i] = CheckConfig{
			Name:          r.Name,
			Type:          CheckTypeDomainExpiry,
			Group:         r.Group,
			Schedule:      r.Schedule,
			Timeout:       r.Timeout,
			Retry:         r.Retry,
			DegradedAfter: r.DegradedAfter,
			DomainExpiry: &DomainExpirationCheckConfig{
				Domain:       r.Domain,
				WarnDays:     warnDays,
				CriticalDays: criticalDays,
			},
		}
	}
	return checks, nil
}

func FindCheckByName(checks []CheckConfig, name string) *CheckConfig {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}
