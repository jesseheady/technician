package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadHTTPChecks(t *testing.T) {
	content := `
- name: Test Site
  type: http
  url: https://example.com
  expected_status: 200
  schedule: "*/30 * * * * *"
  timeout: 5s
  headers:
    X-Custom: value
- name: Test API
  type: http
  url: https://api.example.com/health
  expected_status: 204
  method: HEAD
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "Test Site" {
		t.Errorf("expected name=Test Site, got %s", p.Name)
	}
	if p.Type != CheckTypeHTTP {
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

	p2 := checks[1]
	if p2.HTTP.Method != "HEAD" {
		t.Errorf("expected method=HEAD, got %s", p2.HTTP.Method)
	}
	if p2.HTTP.ExpectedStatus != 204 {
		t.Errorf("expected status=204, got %d", p2.HTTP.ExpectedStatus)
	}
}

func TestLoadChecksMissingPath(t *testing.T) {
	_, err := LoadChecks("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for missing checks path")
	}
}

func TestLoadChecksEmptyDir(t *testing.T) {
	dir := t.TempDir()
	checks, err := LoadChecks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 0 {
		t.Errorf("expected 0 checks, got %d", len(checks))
	}
}

func TestFindCheckByName(t *testing.T) {
	checks := []CheckConfig{
		{Name: "first"},
		{Name: "second"},
		{Name: "third"},
	}

	p := FindCheckByName(checks, "second")
	if p == nil {
		t.Fatal("expected to find check 'second'")
	}
	if p.Name != "second" {
		t.Errorf("expected name=second, got %s", p.Name)
	}

	if FindCheckByName(checks, "missing") != nil {
		t.Error("expected nil for missing check")
	}
}

func TestLoadHTTPChecksEnvExpansion(t *testing.T) {
	os.Setenv("TEST_API_TOKEN", "bearer-xyz")
	defer os.Unsetenv("TEST_API_TOKEN")

	content := `
- name: Authed API
  type: http
  url: https://api.example.com
  expected_status: 200
  headers:
    Authorization: "Bearer ${TEST_API_TOKEN}"
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if checks[0].HTTP.Headers["Authorization"] != "Bearer bearer-xyz" {
		t.Errorf("expected env expansion, got %s", checks[0].HTTP.Headers["Authorization"])
	}
}

func TestLoadTCPChecks(t *testing.T) {
	content := `
- name: Redis
  type: tcp
  host: redis.example.com
  port: 6379
  ip_version: "4"
  schedule: "*/60 * * * * *"
  timeout: 5s
- name: HTTPS Port
  type: tcp
  host: example.com
  port: 443
  tls: true
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "tcp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "Redis" {
		t.Errorf("expected name=Redis, got %s", p.Name)
	}
	if p.Type != CheckTypeTCP {
		t.Errorf("expected type=tcp, got %s", p.Type)
	}
	if p.TCP.Host != "redis.example.com" {
		t.Errorf("expected host=redis.example.com, got %s", p.TCP.Host)
	}
	if p.TCP.Port != 6379 {
		t.Errorf("expected port=6379, got %d", p.TCP.Port)
	}
	if p.TCP.IPVersion != "4" {
		t.Errorf("expected ip_version=4, got %s", p.TCP.IPVersion)
	}

	p2 := checks[1]
	if !p2.TCP.TLS {
		t.Errorf("expected TLS=true for second check")
	}
}

func TestLoadHTTPCheckTLSVersions(t *testing.T) {
	content := `
- name: TLS Pinned
  type: http
  url: https://example.com
  min_tls: "1.2"
  max_tls: "1.3"
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)
	if err := os.WriteFile(filepath.Join(checksDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}
	if checks[0].HTTP.MinTLS != "1.2" || checks[0].HTTP.MaxTLS != "1.3" {
		t.Errorf("expected min_tls=1.2 max_tls=1.3, got %q/%q", checks[0].HTTP.MinTLS, checks[0].HTTP.MaxTLS)
	}
}

func TestLoadCheckInvalidTLSVersion(t *testing.T) {
	cases := map[string]string{
		"unknown version": `
- name: Bad
  type: http
  url: https://example.com
  min_tls: "1.5"
`,
		"min higher than max": `
- name: Inverted
  type: tcp
  host: example.com
  port: 443
  tls: true
  min_tls: "1.3"
  max_tls: "1.2"
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			checksDir := filepath.Join(dir, "checks")
			os.MkdirAll(checksDir, 0o755)
			if err := os.WriteFile(filepath.Join(checksDir, "c.yml"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadChecks(checksDir); err == nil {
				t.Fatal("expected error for invalid TLS version config")
			}
		})
	}
}

func TestLoadSMTPCheckTLSAuth(t *testing.T) {
	content := `
- name: Mail STARTTLS Auth
  type: smtp
  host: smtp.example.com
  port: 587
  start_tls: true
  username: mailer
  password: secret
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)
	if err := os.WriteFile(filepath.Join(checksDir, "smtp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}
	s := checks[0].SMTP
	if !s.StartTLS || s.Username != "mailer" || s.Password != "secret" {
		t.Errorf("expected start_tls+auth parsed, got %+v", s)
	}
}

func TestLoadSMTPCheckInvalidAuth(t *testing.T) {
	cases := map[string]string{
		"auth without start_tls": `
- name: NoTLS
  type: smtp
  host: smtp.example.com
  port: 587
  username: mailer
  password: secret
`,
		"username without password": `
- name: HalfAuth
  type: smtp
  host: smtp.example.com
  port: 587
  start_tls: true
  username: mailer
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			checksDir := filepath.Join(dir, "checks")
			os.MkdirAll(checksDir, 0o755)
			if err := os.WriteFile(filepath.Join(checksDir, "smtp.yml"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadChecks(checksDir); err == nil {
				t.Fatal("expected error for invalid SMTP auth config")
			}
		})
	}
}

func TestLoadDNSChecks(t *testing.T) {
	content := `
- name: Google DNS
  type: dns
  domain: google.com
  server: "8.8.8.8:53"
  record_type: A
  expected:
    - "142.250.80.46"
  timeout: 10s
- name: MX Check
  type: dns
  domain: example.com
  record_type: MX
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "dns.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "Google DNS" {
		t.Errorf("expected name=Google DNS, got %s", p.Name)
	}
	if p.Type != CheckTypeDNS {
		t.Errorf("expected type=dns, got %s", p.Type)
	}
	if p.DNS.Domain != "google.com" {
		t.Errorf("expected domain=google.com, got %s", p.DNS.Domain)
	}
	if p.DNS.Server != "8.8.8.8:53" {
		t.Errorf("expected server=8.8.8.8:53, got %s", p.DNS.Server)
	}
	if p.DNS.RecordType != "A" {
		t.Errorf("expected record_type=A, got %s", p.DNS.RecordType)
	}
	if len(p.DNS.Expected) != 1 || p.DNS.Expected[0] != "142.250.80.46" {
		t.Errorf("expected expected=[142.250.80.46], got %v", p.DNS.Expected)
	}

	p2 := checks[1]
	if p2.DNS.RecordType != "MX" {
		t.Errorf("expected record_type=MX, got %s", p2.DNS.RecordType)
	}
	if p2.DNS.Server != "8.8.8.8:53" {
		t.Errorf("expected default server=8.8.8.8:53, got %s", p2.DNS.Server)
	}
}

func TestLoadICMPChecks(t *testing.T) {
	content := `
- name: Ping Google
  type: icmp
  host: 8.8.8.8
  count: 5
  ip_version: "4"
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "icmp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "Ping Google" {
		t.Errorf("expected name=Ping Google, got %s", p.Name)
	}
	if p.Type != CheckTypeICMP {
		t.Errorf("expected type=icmp, got %s", p.Type)
	}
	if p.ICMP.Host != "8.8.8.8" {
		t.Errorf("expected host=8.8.8.8, got %s", p.ICMP.Host)
	}
	if p.ICMP.Count != 5 {
		t.Errorf("expected count=5, got %d", p.ICMP.Count)
	}
	if p.ICMP.IPVersion != "4" {
		t.Errorf("expected ip_version=4, got %s", p.ICMP.IPVersion)
	}
}

func TestLoadGRPCChecks(t *testing.T) {
	content := `
- name: API Health
  type: grpc
  host: api.example.com:443
  service: myapp
  tls: true
  skip_tls: true
  timeout: 5s
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "grpc.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "API Health" {
		t.Errorf("expected name=API Health, got %s", p.Name)
	}
	if p.Type != CheckTypeGRPC {
		t.Errorf("expected type=grpc, got %s", p.Type)
	}
	if p.GRPC.Host != "api.example.com:443" {
		t.Errorf("expected host=api.example.com:443, got %s", p.GRPC.Host)
	}
	if p.GRPC.Service != "myapp" {
		t.Errorf("expected service=myapp, got %s", p.GRPC.Service)
	}
	if !p.GRPC.TLS {
		t.Errorf("expected TLS=true")
	}
	if !p.GRPC.SkipTLS {
		t.Errorf("expected SkipTLS=true")
	}
}

func TestLoadHTTPChecksWithRetryAndDegraded(t *testing.T) {
	content := `
- name: Retried API
  type: http
  url: https://api.example.com
  expected_status: 200
  follow_redirects: true
  degraded_after: 2s
  retry:
    count: 3
    backoff: exponential
    delay: 500ms
  assertions:
    - type: header_contains
      header: Content-Type
      target: application/json
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}

	p := checks[0]
	if !p.HTTP.FollowRedirects {
		t.Errorf("expected FollowRedirects=true")
	}
	if p.DegradedAfter != 2*time.Second {
		t.Errorf("expected DegradedAfter=2s, got %s", p.DegradedAfter)
	}

	if p.Retry == nil {
		t.Fatal("expected Retry to be set")
	}
	if p.Retry.Count != 3 {
		t.Errorf("expected retry count=3, got %d", p.Retry.Count)
	}
	if p.Retry.Backoff != "exponential" {
		t.Errorf("expected retry backoff=exponential, got %s", p.Retry.Backoff)
	}
	if p.Retry.Delay != 500*time.Millisecond {
		t.Errorf("expected retry delay=500ms, got %s", p.Retry.Delay)
	}

	if len(p.HTTP.Assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(p.HTTP.Assertions))
	}
	a := p.HTTP.Assertions[0]
	if a.Type != "header_contains" {
		t.Errorf("expected assertion type=header_contains, got %s", a.Type)
	}
	if a.Header != "Content-Type" {
		t.Errorf("expected assertion header=Content-Type, got %s", a.Header)
	}
}

func TestLoadSMTPChecksWithRetryAndDegraded(t *testing.T) {
	content := `
- name: Mail Server
  type: smtp
  host: smtp.example.com
  port: 587
  timeout: 5s
  degraded_after: 3s
  retry:
    count: 2
    backoff: linear
    delay: 1s
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "smtp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}

	p := checks[0]
	if p.Type != CheckTypeSMTP {
		t.Errorf("expected type=smtp, got %s", p.Type)
	}
	if p.DegradedAfter != 3*time.Second {
		t.Errorf("expected DegradedAfter=3s, got %s", p.DegradedAfter)
	}
	if p.Retry == nil {
		t.Fatal("expected Retry to be set")
	}
	if p.Retry.Count != 2 {
		t.Errorf("expected retry count=2, got %d", p.Retry.Count)
	}
	if p.Retry.Backoff != "linear" {
		t.Errorf("expected retry backoff=linear, got %s", p.Retry.Backoff)
	}
}

func TestLoadTracerouteChecksWithRetryAndDegraded(t *testing.T) {
	content := `
- name: Trace Route
  type: traceroute
  host: example.com
  max_hops: 20
  degraded_after: 10s
  retry:
    count: 1
    backoff: none
    delay: 2s
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "traceroute.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}

	p := checks[0]
	if p.Type != CheckTypeTraceroute {
		t.Errorf("expected type=traceroute, got %s", p.Type)
	}
	if p.DegradedAfter != 10*time.Second {
		t.Errorf("expected DegradedAfter=10s, got %s", p.DegradedAfter)
	}
	if p.Retry == nil {
		t.Fatal("expected Retry to be set")
	}
	if p.Retry.Count != 1 {
		t.Errorf("expected retry count=1, got %d", p.Retry.Count)
	}
}

func TestEnvExpansionAllCheckTypes(t *testing.T) {
	os.Setenv("TEST_HOST", "expanded.example.com")
	defer os.Unsetenv("TEST_HOST")

	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	// TCP
	os.WriteFile(filepath.Join(checksDir, "tcp.yml"), []byte(`
- name: TCP Test
  type: tcp
  host: ${TEST_HOST}
  port: 6379
`), 0o644)

	// DNS
	os.WriteFile(filepath.Join(checksDir, "dns.yml"), []byte(`
- name: DNS Test
  type: dns
  domain: ${TEST_HOST}
`), 0o644)

	// ICMP
	os.WriteFile(filepath.Join(checksDir, "icmp.yml"), []byte(`
- name: ICMP Test
  type: icmp
  host: ${TEST_HOST}
`), 0o644)

	// gRPC
	os.WriteFile(filepath.Join(checksDir, "grpc.yml"), []byte(`
- name: gRPC Test
  type: grpc
  host: ${TEST_HOST}:443
  tls: true
`), 0o644)

	// SMTP
	os.WriteFile(filepath.Join(checksDir, "smtp.yml"), []byte(`
- name: SMTP Test
  type: smtp
  host: ${TEST_HOST}
  port: 25
`), 0o644)

	// Traceroute
	os.WriteFile(filepath.Join(checksDir, "traceroute.yml"), []byte(`
- name: Trace Test
  type: traceroute
  host: ${TEST_HOST}
`), 0o644)

	// NTP
	os.WriteFile(filepath.Join(checksDir, "ntp.yml"), []byte(`
- name: NTP Test
  type: ntp
  server: ${TEST_HOST}
`), 0o644)

	// UDP
	os.WriteFile(filepath.Join(checksDir, "udp.yml"), []byte(`
- name: UDP Test
  type: udp
  host: ${TEST_HOST}
  port: 5000
  send: PING
`), 0o644)

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range checks {
		switch p.Type {
		case CheckTypeTCP:
			if p.TCP.Host != "expanded.example.com" {
				t.Errorf("TCP: env expansion failed, got host=%s", p.TCP.Host)
			}
		case CheckTypeDNS:
			if p.DNS.Domain != "expanded.example.com" {
				t.Errorf("DNS: env expansion failed, got domain=%s", p.DNS.Domain)
			}
		case CheckTypeICMP:
			if p.ICMP.Host != "expanded.example.com" {
				t.Errorf("ICMP: env expansion failed, got host=%s", p.ICMP.Host)
			}
		case CheckTypeGRPC:
			if p.GRPC.Host != "expanded.example.com:443" {
				t.Errorf("gRPC: env expansion failed, got host=%s", p.GRPC.Host)
			}
		case CheckTypeSMTP:
			if p.SMTP.Host != "expanded.example.com" {
				t.Errorf("SMTP: env expansion failed, got host=%s", p.SMTP.Host)
			}
		case CheckTypeTraceroute:
			if p.Traceroute.Host != "expanded.example.com" {
				t.Errorf("Traceroute: env expansion failed, got host=%s", p.Traceroute.Host)
			}
		case CheckTypeNTP:
			if p.NTP.Server != "expanded.example.com" {
				t.Errorf("NTP: env expansion failed, got server=%s", p.NTP.Server)
			}
		case CheckTypeUDP:
			if p.UDP.Host != "expanded.example.com" {
				t.Errorf("UDP: env expansion failed, got host=%s", p.UDP.Host)
			}
		}
	}
}

func TestLoadNTPChecks(t *testing.T) {
	content := `
- name: Pool NTP
  type: ntp
  server: pool.ntp.org
  schedule: "0 */5 * * * *"
  timeout: 5s
  degraded_after: 100ms
- name: Google NTP
  type: ntp
  server: time.google.com
  port: 123
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "ntp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "Pool NTP" {
		t.Errorf("expected name=Pool NTP, got %s", p.Name)
	}
	if p.Type != CheckTypeNTP {
		t.Errorf("expected type=ntp, got %s", p.Type)
	}
	if p.NTP.Server != "pool.ntp.org" {
		t.Errorf("expected server=pool.ntp.org, got %s", p.NTP.Server)
	}
	if p.NTP.Port != 123 {
		t.Errorf("expected default port=123, got %d", p.NTP.Port)
	}
	if p.DegradedAfter != 100*time.Millisecond {
		t.Errorf("expected DegradedAfter=100ms, got %s", p.DegradedAfter)
	}

	p2 := checks[1]
	if p2.NTP.Server != "time.google.com" {
		t.Errorf("expected server=time.google.com, got %s", p2.NTP.Server)
	}
	if p2.NTP.Port != 123 {
		t.Errorf("expected port=123, got %d", p2.NTP.Port)
	}
}

func TestLoadNTPChecksWithRetryAndEnvExpansion(t *testing.T) {
	os.Setenv("TEST_NTP_SERVER", "time.test.com")
	defer os.Unsetenv("TEST_NTP_SERVER")

	content := `
- name: NTP Retry Test
  type: ntp
  server: ${TEST_NTP_SERVER}
  degraded_after: 200ms
  retry:
    count: 2
    backoff: linear
    delay: 1s
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "ntp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}

	p := checks[0]
	if p.NTP.Server != "time.test.com" {
		t.Errorf("env expansion failed, got server=%s", p.NTP.Server)
	}
	if p.Retry == nil {
		t.Fatal("expected Retry to be set")
	}
	if p.Retry.Count != 2 {
		t.Errorf("expected retry count=2, got %d", p.Retry.Count)
	}
	if p.Retry.Backoff != "linear" {
		t.Errorf("expected retry backoff=linear, got %s", p.Retry.Backoff)
	}
	if p.DegradedAfter != 200*time.Millisecond {
		t.Errorf("expected DegradedAfter=200ms, got %s", p.DegradedAfter)
	}
}

func TestLoadHTTPCheckHeaderAssertionValidation(t *testing.T) {
	content := `
- name: Bad Header Assert
  type: http
  url: https://example.com
  assertions:
    - type: header_contains
      target: something
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadChecks(checksDir)
	if err == nil {
		t.Fatal("expected error for missing header field")
	}
	if !strings.Contains(err.Error(), "header is required") {
		t.Errorf("expected error to contain 'header is required', got: %s", err.Error())
	}
}

func TestLoadHTTPCheckAuthFields(t *testing.T) {
	content := `
- name: Basic Auth
  type: http
  url: https://example.com
  basic_auth:
    username: alice
    password: s3cret
- name: Bearer Auth
  type: http
  url: https://example.com
  bearer_token: abc.def.ghi
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)
	if err := os.WriteFile(filepath.Join(checksDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]CheckConfig{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if ba := byName["Basic Auth"].HTTP.BasicAuth; ba == nil || ba.Username != "alice" || ba.Password != "s3cret" {
		t.Errorf("expected basic_auth alice/s3cret, got %+v", ba)
	}
	if tok := byName["Bearer Auth"].HTTP.BearerToken; tok != "abc.def.ghi" {
		t.Errorf("expected bearer_token abc.def.ghi, got %q", tok)
	}
}

func TestLoadHTTPCheckAuthMutuallyExclusive(t *testing.T) {
	content := `
- name: Both Auth
  type: http
  url: https://example.com
  bearer_token: abc.def.ghi
  basic_auth:
    username: alice
    password: s3cret
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)
	if err := os.WriteFile(filepath.Join(checksDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadChecks(checksDir)
	if err == nil {
		t.Fatal("expected error when both basic_auth and bearer_token are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected error to contain 'mutually exclusive', got: %s", err.Error())
	}
}

func TestLoadTLSChecks(t *testing.T) {
	content := `
- name: API Cert
  type: tls
  host: api.example.com:443
  schedule: "0 0 */6 * * *"
  timeout: 10s
  warn_days: 30
  critical_days: 7
- name: Web Cert
  type: tls
  host: www.example.com
  check_expiry: false
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "tls.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "API Cert" {
		t.Errorf("expected name=API Cert, got %s", p.Name)
	}
	if p.Type != CheckTypeTLS {
		t.Errorf("expected type=tls, got %s", p.Type)
	}
	if p.TLS.Host != "api.example.com:443" {
		t.Errorf("expected host=api.example.com:443, got %s", p.TLS.Host)
	}
	if !p.TLS.CheckExpiry {
		t.Error("expected CheckExpiry=true by default")
	}
	if p.TLS.WarnDays != 30 {
		t.Errorf("expected WarnDays=30, got %d", p.TLS.WarnDays)
	}
	if p.TLS.CriticalDays != 7 {
		t.Errorf("expected CriticalDays=7, got %d", p.TLS.CriticalDays)
	}
	if p.Schedule != "0 0 */6 * * *" {
		t.Errorf("expected schedule, got %s", p.Schedule)
	}

	p2 := checks[1]
	if p2.TLS.Host != "www.example.com" {
		t.Errorf("expected host=www.example.com, got %s", p2.TLS.Host)
	}
	if p2.TLS.CheckExpiry {
		t.Error("expected CheckExpiry=false when explicitly set")
	}
	// Defaults still applied for warn/critical
	if p2.TLS.WarnDays != 30 {
		t.Errorf("expected default WarnDays=30, got %d", p2.TLS.WarnDays)
	}
	if p2.TLS.CriticalDays != 7 {
		t.Errorf("expected default CriticalDays=7, got %d", p2.TLS.CriticalDays)
	}
}

func TestLoadUDPChecks(t *testing.T) {
	content := `
- name: DNS over UDP
  type: udp
  host: 8.8.8.8
  port: 53
  send_hex: "002a0100"
  schedule: "*/30 * * * * *"
  timeout: 5s
- name: StatsD
  type: udp
  host: statsd.internal
  port: 8125
  send: "test.metric:1|c"
  expect_response: false
  max_response_bytes: 1024
  degraded_after: 1s
  retry:
    count: 2
    backoff: linear
    delay: 500ms
`
	dir := t.TempDir()
	checksDir := filepath.Join(dir, "checks")
	os.MkdirAll(checksDir, 0o755)

	if err := os.WriteFile(filepath.Join(checksDir, "udp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	checks, err := LoadChecks(checksDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}

	p := checks[0]
	if p.Name != "DNS over UDP" {
		t.Errorf("expected name=DNS over UDP, got %s", p.Name)
	}
	if p.Type != CheckTypeUDP {
		t.Errorf("expected type=udp, got %s", p.Type)
	}
	if p.UDP.Host != "8.8.8.8" {
		t.Errorf("expected host=8.8.8.8, got %s", p.UDP.Host)
	}
	if p.UDP.Port != 53 {
		t.Errorf("expected port=53, got %d", p.UDP.Port)
	}
	if p.UDP.SendHex != "002a0100" {
		t.Errorf("expected send_hex=002a0100, got %s", p.UDP.SendHex)
	}
	if p.UDP.MaxResponseBytes != 4096 {
		t.Errorf("expected default max_response_bytes=4096, got %d", p.UDP.MaxResponseBytes)
	}

	p2 := checks[1]
	if p2.UDP.Send != "test.metric:1|c" {
		t.Errorf("expected send=test.metric:1|c, got %s", p2.UDP.Send)
	}
	if p2.UDP.ExpectResponse == nil || *p2.UDP.ExpectResponse != false {
		t.Error("expected expect_response=false")
	}
	if p2.UDP.MaxResponseBytes != 1024 {
		t.Errorf("expected max_response_bytes=1024, got %d", p2.UDP.MaxResponseBytes)
	}
	if p2.DegradedAfter != 1*time.Second {
		t.Errorf("expected DegradedAfter=1s, got %s", p2.DegradedAfter)
	}
	if p2.Retry == nil {
		t.Fatal("expected Retry to be set")
	}
	if p2.Retry.Count != 2 {
		t.Errorf("expected retry count=2, got %d", p2.Retry.Count)
	}
	if p2.Retry.Backoff != "linear" {
		t.Errorf("expected retry backoff=linear, got %s", p2.Retry.Backoff)
	}
}
