package config

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var validAssertionTypes = map[string]bool{
	"contains": true, "not_contains": true, "regex": true,
	"header_contains": true, "header_not_contains": true, "header_regex": true,
}

// TLSVersion maps a config string ("1.0".."1.3") to a crypto/tls version
// constant. The second return value is false for unrecognized strings.
func TLSVersion(s string) (uint16, bool) {
	switch s {
	case "1.0":
		return tls.VersionTLS10, true
	case "1.1":
		return tls.VersionTLS11, true
	case "1.2":
		return tls.VersionTLS12, true
	case "1.3":
		return tls.VersionTLS13, true
	default:
		return 0, false
	}
}

// validateTLSVersions checks that min_tls/max_tls are recognized values and
// that min does not exceed max. Empty strings mean "unset" and are allowed.
func validateTLSVersions(name, minTLS, maxTLS string) error {
	var minV, maxV uint16
	if minTLS != "" {
		v, ok := TLSVersion(minTLS)
		if !ok {
			return fmt.Errorf("check %q: invalid min_tls %q (want 1.0, 1.1, 1.2, or 1.3)", name, minTLS)
		}
		minV = v
	}
	if maxTLS != "" {
		v, ok := TLSVersion(maxTLS)
		if !ok {
			return fmt.Errorf("check %q: invalid max_tls %q (want 1.0, 1.1, 1.2, or 1.3)", name, maxTLS)
		}
		maxV = v
	}
	if minV != 0 && maxV != 0 && minV > maxV {
		return fmt.Errorf("check %q: min_tls %q is higher than max_tls %q", name, minTLS, maxTLS)
	}
	return nil
}

type CheckType string

const (
	CheckTypeHTTP         CheckType = "http"
	CheckTypeSMTP         CheckType = "smtp"
	CheckTypeTraceroute   CheckType = "traceroute"
	CheckTypePlaywright   CheckType = "playwright"
	CheckTypeTCP          CheckType = "tcp"
	CheckTypeDNS          CheckType = "dns"
	CheckTypeICMP         CheckType = "icmp"
	CheckTypeGRPC         CheckType = "grpc"
	CheckTypeNTP          CheckType = "ntp"
	CheckTypeTLS          CheckType = "tls"
	CheckTypeUDP          CheckType = "udp"
	CheckTypeBGP          CheckType = "bgp"
	CheckTypeDomainExpiry CheckType = "domain_expiry"
	CheckTypeWebSocket    CheckType = "websocket"
)

type RetryPolicy struct {
	Count   int           `yaml:"count"`   // number of retries (0 = no retry)
	Backoff string        `yaml:"backoff"` // "none", "linear", "exponential"
	Delay   time.Duration `yaml:"delay"`   // base delay between retries
}

type CheckConfig struct {
	Name          string                       `yaml:"name"`
	Type          CheckType                    `yaml:"-"`
	Group         string                       `yaml:"group"`
	Tags          []string                     `yaml:"tags"`
	Schedule      string                       `yaml:"schedule"`
	Timeout       time.Duration                `yaml:"timeout"`
	Retry         *RetryPolicy                 `yaml:"retry"`
	DegradedAfter time.Duration                `yaml:"degraded_after"` // response time threshold for "degraded" state
	HTTP          *HTTPCheckConfig             `yaml:"-"`
	SMTP          *SMTPCheckConfig             `yaml:"-"`
	Traceroute    *TracerouteCheckConfig       `yaml:"-"`
	Playwright    *PlaywrightCheckConfig       `yaml:"-"`
	TCP           *TCPCheckConfig              `yaml:"-"`
	DNS           *DNSCheckConfig              `yaml:"-"`
	ICMP          *ICMPCheckConfig             `yaml:"-"`
	GRPC          *GRPCCheckConfig             `yaml:"-"`
	NTP           *NTPCheckConfig              `yaml:"-"`
	TLS           *TLSCheckConfig              `yaml:"-"`
	UDP           *UDPCheckConfig              `yaml:"-"`
	BGP           *BGPCheckConfig              `yaml:"-"`
	DomainExpiry  *DomainExpirationCheckConfig `yaml:"-"`
	WebSocket     *WebSocketCheckConfig        `yaml:"-"`
}

// Target returns the canonical hostname or IP that this check targets.
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
	case CheckTypeBGP:
		if p.BGP != nil {
			raw = p.BGP.Prefix
		}
	case CheckTypeDomainExpiry:
		if p.DomainExpiry != nil {
			raw = p.DomainExpiry.Domain
		}
	case CheckTypePlaywright:
		if p.Playwright != nil {
			if u, err := url.Parse(p.Playwright.BaseURL); err == nil {
				raw = u.Hostname()
			}
		}
	case CheckTypeWebSocket:
		if p.WebSocket != nil {
			if u, err := url.Parse(p.WebSocket.URL); err == nil {
				raw = u.Hostname()
			}
		}
	}
	if raw == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(raw)
	if err != nil {
		return raw
	}
	return host
}

type Assertion struct {
	Type   string `yaml:"type"`   // contains, not_contains, regex, header_contains, header_not_contains, header_regex
	Header string `yaml:"header"` // required for header_* types
	Target string `yaml:"target"` // string to match or regex pattern
}

type HTTPCheckConfig struct {
	URL             string            `yaml:"url"`
	Method          string            `yaml:"method"`
	ExpectedStatus  int               `yaml:"expected_status"`
	Headers         map[string]string `yaml:"headers"`
	Body            string            `yaml:"body"`
	SkipTLS         bool              `yaml:"skip_tls"`
	FollowRedirects bool              `yaml:"follow_redirects"`
	IPVersion       string            `yaml:"ip_version"`   // "4", "6", or "" (any)
	MinTLS          string            `yaml:"min_tls"`      // "1.0".."1.3", or "" (Go default)
	MaxTLS          string            `yaml:"max_tls"`      // "1.0".."1.3", or "" (Go default)
	BasicAuth       *BasicAuth        `yaml:"basic_auth"`   // username/password for HTTP Basic auth
	BearerToken     string            `yaml:"bearer_token"` // token for "Authorization: Bearer" header
	Proxy           string            `yaml:"proxy"`        // proxy URL, e.g. "http://proxy.example.com:8080" (empty = no proxy)
	Assertions      []Assertion       `yaml:"assertions"`
}

// BasicAuth holds credentials for HTTP Basic authentication.
type BasicAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type TCPCheckConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	IPVersion  string `yaml:"ip_version"` // "4", "6", or "" (any)
	TLS        bool   `yaml:"tls"`
	MinTLS     string `yaml:"min_tls"`     // "1.0".."1.3", or "" (Go default); applies when tls is true
	MaxTLS     string `yaml:"max_tls"`     // "1.0".."1.3", or "" (Go default); applies when tls is true
	Send       string `yaml:"send"`        // optional bytes to send
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
	IPVersion        string `yaml:"ip_version"`         // "4", "6", or "" (any)
	Send             string `yaml:"send"`               // payload to send (plain text)
	SendHex          string `yaml:"send_hex"`           // payload to send (hex-encoded bytes)
	ExpectResponse   *bool  `yaml:"expect_response"`    // nil defaults to true
	ExpectRecv       string `yaml:"expect_recv"`        // expected substring in response
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
	Host     string        `yaml:"host"`
	Port     int           `yaml:"port"`
	Timeout  time.Duration `yaml:"timeout"`
	StartTLS bool          `yaml:"start_tls"` // negotiate STARTTLS after EHLO
	SkipTLS  bool          `yaml:"skip_tls"`  // skip TLS cert verification (with start_tls)
	Username string        `yaml:"username"`  // SMTP AUTH username (requires start_tls)
	Password string        `yaml:"password"`  // SMTP AUTH password
}

type TracerouteCheckConfig struct {
	Host    string `yaml:"host"`
	MaxHops int    `yaml:"max_hops"`
	Count   int    `yaml:"count"`
}

type WebSocketCheckConfig struct {
	URL        string            `yaml:"url"`         // ws:// or wss:// endpoint
	Send       string            `yaml:"send"`        // optional message to send after connect
	ExpectRecv string            `yaml:"expect_recv"` // optional substring expected in the reply
	SkipTLS    bool              `yaml:"skip_tls"`    // skip TLS cert verification (wss)
	Headers    map[string]string `yaml:"headers"`     // optional handshake headers (e.g. auth)
}

type PlaywrightCheckConfig struct {
	Script        string `yaml:"script"`
	Authenticator string `yaml:"authenticator"`
	BaseURL       string `yaml:"base_url"`
	Video         bool   `yaml:"video"`
	Network       string `yaml:"network"` // 4g, 3g, slow-3g, or empty (no throttling)
	Device        string `yaml:"device"`  // Playwright device name, e.g. "iPhone 14", "Pixel 7"
}

// checkYAML is the unified YAML representation for all check types.
// The `type` field determines which type-specific fields are relevant.
// Unknown fields for a given type are silently ignored by the YAML parser.
type checkYAML struct {
	// Common fields
	Name          string        `yaml:"name"`
	Type          CheckType     `yaml:"type"`
	Group         string        `yaml:"group"`
	Tags          []string      `yaml:"tags"`
	Schedule      string        `yaml:"schedule"`
	Timeout       time.Duration `yaml:"timeout"`
	Retry         *RetryPolicy  `yaml:"retry"`
	DegradedAfter time.Duration `yaml:"degraded_after"`

	// HTTP
	URL             string            `yaml:"url"`
	Method          string            `yaml:"method"`
	ExpectedStatus  int               `yaml:"expected_status"`
	Headers         map[string]string `yaml:"headers"`
	Body            string            `yaml:"body"`
	SkipTLS         bool              `yaml:"skip_tls"`
	FollowRedirects bool              `yaml:"follow_redirects"`
	BasicAuth       *BasicAuth        `yaml:"basic_auth"`
	BearerToken     string            `yaml:"bearer_token"`
	Proxy           string            `yaml:"proxy"`
	Assertions      []Assertion       `yaml:"assertions"`

	// HTTP / TCP — TLS version constraints
	MinTLS string `yaml:"min_tls"`
	MaxTLS string `yaml:"max_tls"`

	// TCP / SMTP / ICMP / Traceroute / gRPC / NTP / TLS / Domain Expiry
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	IPVersion string `yaml:"ip_version"`

	// TCP / UDP
	Send       string `yaml:"send"`
	ExpectRecv string `yaml:"expect_recv"`
	TLS        bool   `yaml:"tls"`

	// SMTP
	StartTLS bool   `yaml:"start_tls"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// DNS
	Domain     string   `yaml:"domain"`
	Server     string   `yaml:"server"`
	RecordType string   `yaml:"record_type"`
	Expected   []string `yaml:"expected"`

	// ICMP / Traceroute
	Count   int `yaml:"count"`
	MaxHops int `yaml:"max_hops"`

	// gRPC
	Service string `yaml:"service"`

	// TLS / Domain Expiry
	CheckExpiry  *bool `yaml:"check_expiry"`
	WarnDays     int   `yaml:"warn_days"`
	CriticalDays int   `yaml:"critical_days"`

	// UDP
	SendHex          string `yaml:"send_hex"`
	ExpectResponse   *bool  `yaml:"expect_response"`
	MaxResponseBytes int    `yaml:"max_response_bytes"`

	// BGP
	Prefix         string `yaml:"prefix"`
	ExpectedOrigin int    `yaml:"expected_origin"`

	// Playwright
	Script        string `yaml:"script"`
	Authenticator string `yaml:"authenticator"`
	BaseURL       string `yaml:"base_url"`
	Video         bool   `yaml:"video"`
	Network       string `yaml:"network"`
	Device        string `yaml:"device"`
}

// LoadChecks loads check definitions from a file or directory. If path is a
// file, it parses it as a YAML list of checks. If path is a directory, it
// reads all .yml and .yaml files, merges them into a single list, and returns
// the combined result. This supports both single-file configs and directory-
// based organization for larger deployments.
func LoadChecks(path string) ([]CheckConfig, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var checks []CheckConfig
	if info.IsDir() {
		checks, err = loadChecksFromDir(path)
	} else {
		checks, err = loadChecksFromFile(path)
	}
	if err != nil {
		return nil, err
	}

	// Apply defaults
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

func loadChecksFromDir(dir string) ([]CheckConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading checks directory: %w", err)
	}

	var all []CheckConfig
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		checks, err := loadChecksFromFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", name, err)
		}
		all = append(all, checks...)
	}
	return all, nil
}

func loadChecksFromFile(path string) ([]CheckConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnvVars(string(data))

	var raw []checkYAML
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	checks := make([]CheckConfig, 0, len(raw))
	for i, r := range raw {
		c, err := convertCheck(r, path)
		if err != nil {
			return nil, fmt.Errorf("%s[%d] (%s): %w", path, i, r.Name, err)
		}
		checks = append(checks, c)
	}
	return checks, nil
}

// convertCheck transforms a unified YAML entry into a typed CheckConfig.
func convertCheck(r checkYAML, sourcePath string) (CheckConfig, error) {
	if r.Type == "" {
		return CheckConfig{}, fmt.Errorf("check %q: type is required", r.Name)
	}

	c := CheckConfig{
		Name:          r.Name,
		Type:          r.Type,
		Group:         r.Group,
		Tags:          r.Tags,
		Schedule:      r.Schedule,
		Timeout:       r.Timeout,
		Retry:         r.Retry,
		DegradedAfter: r.DegradedAfter,
	}

	switch r.Type {
	case CheckTypeHTTP:
		if r.ExpectedStatus == 0 {
			r.ExpectedStatus = 200
		}
		if r.Method == "" {
			r.Method = "GET"
		}
		c.HTTP = &HTTPCheckConfig{
			URL:             r.URL,
			Method:          r.Method,
			ExpectedStatus:  r.ExpectedStatus,
			Headers:         r.Headers,
			Body:            r.Body,
			SkipTLS:         r.SkipTLS,
			FollowRedirects: r.FollowRedirects,
			IPVersion:       r.IPVersion,
			MinTLS:          r.MinTLS,
			MaxTLS:          r.MaxTLS,
			BasicAuth:       r.BasicAuth,
			BearerToken:     r.BearerToken,
			Proxy:           r.Proxy,
			Assertions:      r.Assertions,
		}
		if err := validateTLSVersions(c.Name, c.HTTP.MinTLS, c.HTTP.MaxTLS); err != nil {
			return CheckConfig{}, err
		}
		if c.HTTP.BasicAuth != nil && c.HTTP.BearerToken != "" {
			return CheckConfig{}, fmt.Errorf("check %q: basic_auth and bearer_token are mutually exclusive", c.Name)
		}
		if c.HTTP.Proxy != "" {
			u, err := url.Parse(c.HTTP.Proxy)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return CheckConfig{}, fmt.Errorf("check %q: invalid proxy URL %q", c.Name, c.HTTP.Proxy)
			}
		}
		if err := validateAssertions(c.Name, c.HTTP.Assertions); err != nil {
			return CheckConfig{}, err
		}

	case CheckTypeTCP:
		c.TCP = &TCPCheckConfig{
			Host:       r.Host,
			Port:       r.Port,
			IPVersion:  r.IPVersion,
			TLS:        r.TLS,
			MinTLS:     r.MinTLS,
			MaxTLS:     r.MaxTLS,
			Send:       r.Send,
			ExpectRecv: r.ExpectRecv,
		}
		if err := validateTLSVersions(c.Name, c.TCP.MinTLS, c.TCP.MaxTLS); err != nil {
			return CheckConfig{}, err
		}

	case CheckTypeDNS:
		if r.RecordType == "" {
			r.RecordType = "A"
		}
		if r.Server == "" {
			r.Server = "8.8.8.8:53"
		}
		c.DNS = &DNSCheckConfig{
			Domain:     r.Domain,
			Server:     r.Server,
			RecordType: r.RecordType,
			Expected:   r.Expected,
		}

	case CheckTypeICMP:
		if r.Count == 0 {
			r.Count = 3
		}
		c.ICMP = &ICMPCheckConfig{
			Host:      r.Host,
			Count:     r.Count,
			IPVersion: r.IPVersion,
		}

	case CheckTypeGRPC:
		c.GRPC = &GRPCCheckConfig{
			Host:    r.Host,
			Service: r.Service,
			TLS:     r.TLS,
			SkipTLS: r.SkipTLS,
		}

	case CheckTypeNTP:
		if r.Port == 0 {
			r.Port = 123
		}
		c.NTP = &NTPCheckConfig{
			Server: r.Server,
			Port:   r.Port,
		}

	case CheckTypeTLS:
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
		c.TLS = &TLSCheckConfig{
			Host:         r.Host,
			CheckExpiry:  checkExpiry,
			WarnDays:     warnDays,
			CriticalDays: criticalDays,
		}

	case CheckTypeUDP:
		maxResp := r.MaxResponseBytes
		if maxResp == 0 {
			maxResp = 4096
		}
		c.UDP = &UDPCheckConfig{
			Host:             r.Host,
			Port:             r.Port,
			IPVersion:        r.IPVersion,
			Send:             r.Send,
			SendHex:          r.SendHex,
			ExpectResponse:   r.ExpectResponse,
			ExpectRecv:       r.ExpectRecv,
			MaxResponseBytes: maxResp,
		}

	case CheckTypeBGP:
		if _, err := netip.ParsePrefix(r.Prefix); err != nil {
			return c, fmt.Errorf("check %q: invalid prefix %q (want CIDR notation, for example 203.0.113.0/24)", r.Name, r.Prefix)
		}
		if r.ExpectedOrigin <= 0 {
			return c, fmt.Errorf("check %q: expected_origin is required. Without it the check reports prefix visibility only, and it cannot detect a hijack", r.Name)
		}
		c.BGP = &BGPCheckConfig{
			Prefix:         r.Prefix,
			ExpectedOrigin: r.ExpectedOrigin,
		}

	case CheckTypeDomainExpiry:
		warnDays := r.WarnDays
		if warnDays == 0 {
			warnDays = 30
		}
		criticalDays := r.CriticalDays
		if criticalDays == 0 {
			criticalDays = 7
		}
		c.DomainExpiry = &DomainExpirationCheckConfig{
			Domain:       r.Domain,
			WarnDays:     warnDays,
			CriticalDays: criticalDays,
		}

	case CheckTypeSMTP:
		if r.Port == 0 {
			r.Port = 25
		}
		c.SMTP = &SMTPCheckConfig{
			Host:     r.Host,
			Port:     r.Port,
			Timeout:  r.Timeout,
			StartTLS: r.StartTLS,
			SkipTLS:  r.SkipTLS,
			Username: r.Username,
			Password: r.Password,
		}
		if (c.SMTP.Username != "") != (c.SMTP.Password != "") {
			return CheckConfig{}, fmt.Errorf("check %q: smtp username and password must be set together", c.Name)
		}
		if c.SMTP.Username != "" && !c.SMTP.StartTLS {
			return CheckConfig{}, fmt.Errorf("check %q: smtp authentication requires start_tls", c.Name)
		}

	case CheckTypeTraceroute:
		if r.MaxHops == 0 {
			r.MaxHops = 30
		}
		if r.Count == 0 {
			r.Count = 3
		}
		c.Traceroute = &TracerouteCheckConfig{
			Host:    r.Host,
			MaxHops: r.MaxHops,
			Count:   r.Count,
		}

	case CheckTypePlaywright:
		script := r.Script
		if !filepath.IsAbs(script) && script != "" {
			script = filepath.Join(filepath.Dir(sourcePath), script)
		}
		c.Playwright = &PlaywrightCheckConfig{
			Script:        script,
			Authenticator: r.Authenticator,
			BaseURL:       r.BaseURL,
			Video:         r.Video,
			Network:       r.Network,
			Device:        r.Device,
		}

	case CheckTypeWebSocket:
		c.WebSocket = &WebSocketCheckConfig{
			URL:        r.URL,
			Send:       r.Send,
			ExpectRecv: r.ExpectRecv,
			SkipTLS:    r.SkipTLS,
			Headers:    r.Headers,
		}
		u, err := url.Parse(c.WebSocket.URL)
		if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
			return CheckConfig{}, fmt.Errorf("check %q: websocket url must be a ws:// or wss:// URL, got %q", c.Name, c.WebSocket.URL)
		}

	default:
		return CheckConfig{}, fmt.Errorf("unknown check type %q", r.Type)
	}

	return c, nil
}

func validateAssertions(checkName string, assertions []Assertion) error {
	for j, a := range assertions {
		if !validAssertionTypes[a.Type] {
			return fmt.Errorf("check %q assertion[%d]: invalid type %q", checkName, j, a.Type)
		}
		if a.Target == "" {
			return fmt.Errorf("check %q assertion[%d]: target is required", checkName, j)
		}
		if strings.HasPrefix(a.Type, "header_") && a.Header == "" {
			return fmt.Errorf("check %q assertion[%d]: header is required for type %q", checkName, j, a.Type)
		}
		if a.Type == "regex" || a.Type == "header_regex" {
			if _, err := regexp.Compile(a.Target); err != nil {
				return fmt.Errorf("check %q assertion[%d]: invalid regex %q: %w", checkName, j, a.Target, err)
			}
		}
	}
	return nil
}

func FindCheckByName(checks []CheckConfig, name string) *CheckConfig {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}
