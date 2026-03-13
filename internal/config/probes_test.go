package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestLoadTCPProbes(t *testing.T) {
	content := `
- name: Redis
  host: redis.example.com
  port: 6379
  ip_version: "4"
  schedule: "*/60 * * * * *"
  timeout: 5s
- name: HTTPS Port
  host: example.com
  port: 443
  tls: true
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "tcp.yml"), []byte(content), 0o644); err != nil {
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
	if p.Name != "Redis" {
		t.Errorf("expected name=Redis, got %s", p.Name)
	}
	if p.Type != ProbeTypeTCP {
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

	p2 := probes[1]
	if !p2.TCP.TLS {
		t.Errorf("expected TLS=true for second probe")
	}
}

func TestLoadDNSProbes(t *testing.T) {
	content := `
- name: Google DNS
  domain: google.com
  server: "8.8.8.8:53"
  record_type: A
  expected:
    - "142.250.80.46"
  timeout: 10s
- name: MX Check
  domain: example.com
  record_type: MX
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "dns.yml"), []byte(content), 0o644); err != nil {
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
	if p.Name != "Google DNS" {
		t.Errorf("expected name=Google DNS, got %s", p.Name)
	}
	if p.Type != ProbeTypeDNS {
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

	p2 := probes[1]
	if p2.DNS.RecordType != "MX" {
		t.Errorf("expected record_type=MX, got %s", p2.DNS.RecordType)
	}
	if p2.DNS.Server != "8.8.8.8:53" {
		t.Errorf("expected default server=8.8.8.8:53, got %s", p2.DNS.Server)
	}
}

func TestLoadICMPProbes(t *testing.T) {
	content := `
- name: Ping Google
  host: 8.8.8.8
  count: 5
  ip_version: "4"
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "icmp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}

	p := probes[0]
	if p.Name != "Ping Google" {
		t.Errorf("expected name=Ping Google, got %s", p.Name)
	}
	if p.Type != ProbeTypeICMP {
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

func TestLoadGRPCProbes(t *testing.T) {
	content := `
- name: API Health
  host: api.example.com:443
  service: myapp
  tls: true
  skip_tls: true
  timeout: 5s
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "grpc.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}

	p := probes[0]
	if p.Name != "API Health" {
		t.Errorf("expected name=API Health, got %s", p.Name)
	}
	if p.Type != ProbeTypeGRPC {
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

func TestLoadHTTPProbesWithRetryAndDegraded(t *testing.T) {
	content := `
- name: Retried API
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
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}

	p := probes[0]
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

func TestLoadSMTPProbesWithRetryAndDegraded(t *testing.T) {
	content := `
- name: Mail Server
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
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "smtp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}

	p := probes[0]
	if p.Type != ProbeTypeSMTP {
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

func TestLoadTracerouteProbesWithRetryAndDegraded(t *testing.T) {
	content := `
- name: Trace Route
  host: example.com
  max_hops: 20
  degraded_after: 10s
  retry:
    count: 1
    backoff: none
    delay: 2s
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "traceroute.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}

	p := probes[0]
	if p.Type != ProbeTypeTraceroute {
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

func TestEnvExpansionAllProbeTypes(t *testing.T) {
	os.Setenv("TEST_HOST", "expanded.example.com")
	defer os.Unsetenv("TEST_HOST")

	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	// TCP
	os.WriteFile(filepath.Join(probesDir, "tcp.yml"), []byte(`
- name: TCP Test
  host: ${TEST_HOST}
  port: 6379
`), 0o644)

	// DNS
	os.WriteFile(filepath.Join(probesDir, "dns.yml"), []byte(`
- name: DNS Test
  domain: ${TEST_HOST}
`), 0o644)

	// ICMP
	os.WriteFile(filepath.Join(probesDir, "icmp.yml"), []byte(`
- name: ICMP Test
  host: ${TEST_HOST}
`), 0o644)

	// gRPC
	os.WriteFile(filepath.Join(probesDir, "grpc.yml"), []byte(`
- name: gRPC Test
  host: ${TEST_HOST}:443
  tls: true
`), 0o644)

	// SMTP
	os.WriteFile(filepath.Join(probesDir, "smtp.yml"), []byte(`
- name: SMTP Test
  host: ${TEST_HOST}
  port: 25
`), 0o644)

	// Traceroute
	os.WriteFile(filepath.Join(probesDir, "traceroute.yml"), []byte(`
- name: Trace Test
  host: ${TEST_HOST}
`), 0o644)

	// NTP
	os.WriteFile(filepath.Join(probesDir, "ntp.yml"), []byte(`
- name: NTP Test
  server: ${TEST_HOST}
`), 0o644)

	// UDP
	os.WriteFile(filepath.Join(probesDir, "udp.yml"), []byte(`
- name: UDP Test
  host: ${TEST_HOST}
  port: 5000
  send: PING
`), 0o644)

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range probes {
		switch p.Type {
		case ProbeTypeTCP:
			if p.TCP.Host != "expanded.example.com" {
				t.Errorf("TCP: env expansion failed, got host=%s", p.TCP.Host)
			}
		case ProbeTypeDNS:
			if p.DNS.Domain != "expanded.example.com" {
				t.Errorf("DNS: env expansion failed, got domain=%s", p.DNS.Domain)
			}
		case ProbeTypeICMP:
			if p.ICMP.Host != "expanded.example.com" {
				t.Errorf("ICMP: env expansion failed, got host=%s", p.ICMP.Host)
			}
		case ProbeTypeGRPC:
			if p.GRPC.Host != "expanded.example.com:443" {
				t.Errorf("gRPC: env expansion failed, got host=%s", p.GRPC.Host)
			}
		case ProbeTypeSMTP:
			if p.SMTP.Host != "expanded.example.com" {
				t.Errorf("SMTP: env expansion failed, got host=%s", p.SMTP.Host)
			}
		case ProbeTypeTraceroute:
			if p.Traceroute.Host != "expanded.example.com" {
				t.Errorf("Traceroute: env expansion failed, got host=%s", p.Traceroute.Host)
			}
		case ProbeTypeNTP:
			if p.NTP.Server != "expanded.example.com" {
				t.Errorf("NTP: env expansion failed, got server=%s", p.NTP.Server)
			}
		case ProbeTypeUDP:
			if p.UDP.Host != "expanded.example.com" {
				t.Errorf("UDP: env expansion failed, got host=%s", p.UDP.Host)
			}
		}
	}
}

func TestLoadNTPProbes(t *testing.T) {
	content := `
- name: Pool NTP
  server: pool.ntp.org
  schedule: "0 */5 * * * *"
  timeout: 5s
  degraded_after: 100ms
- name: Google NTP
  server: time.google.com
  port: 123
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "ntp.yml"), []byte(content), 0o644); err != nil {
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
	if p.Name != "Pool NTP" {
		t.Errorf("expected name=Pool NTP, got %s", p.Name)
	}
	if p.Type != ProbeTypeNTP {
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

	p2 := probes[1]
	if p2.NTP.Server != "time.google.com" {
		t.Errorf("expected server=time.google.com, got %s", p2.NTP.Server)
	}
	if p2.NTP.Port != 123 {
		t.Errorf("expected port=123, got %d", p2.NTP.Port)
	}
}

func TestLoadNTPProbesWithRetryAndEnvExpansion(t *testing.T) {
	os.Setenv("TEST_NTP_SERVER", "time.test.com")
	defer os.Unsetenv("TEST_NTP_SERVER")

	content := `
- name: NTP Retry Test
  server: ${TEST_NTP_SERVER}
  degraded_after: 200ms
  retry:
    count: 2
    backoff: linear
    delay: 1s
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "ntp.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	probes, err := LoadProbes(probesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}

	p := probes[0]
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

func TestLoadHTTPProbeHeaderAssertionValidation(t *testing.T) {
	content := `
- name: Bad Header Assert
  url: https://example.com
  assertions:
    - type: header_contains
      target: something
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "http.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadProbes(probesDir)
	if err == nil {
		t.Fatal("expected error for missing header field")
	}
	if !strings.Contains(err.Error(), "header is required") {
		t.Errorf("expected error to contain 'header is required', got: %s", err.Error())
	}
}

func TestLoadTLSProbes(t *testing.T) {
	content := `
- name: API Cert
  host: api.example.com:443
  schedule: "0 0 */6 * * *"
  timeout: 10s
  warn_days: 30
  critical_days: 7
- name: Web Cert
  host: www.example.com
  check_expiry: false
`
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "tls.yml"), []byte(content), 0o644); err != nil {
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
	if p.Name != "API Cert" {
		t.Errorf("expected name=API Cert, got %s", p.Name)
	}
	if p.Type != ProbeTypeTLS {
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

	p2 := probes[1]
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

func TestLoadUDPProbes(t *testing.T) {
	content := `
- name: DNS over UDP
  host: 8.8.8.8
  port: 53
  send_hex: "002a0100"
  schedule: "*/30 * * * * *"
  timeout: 5s
- name: StatsD
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
	probesDir := filepath.Join(dir, "probes")
	os.MkdirAll(probesDir, 0o755)

	if err := os.WriteFile(filepath.Join(probesDir, "udp.yml"), []byte(content), 0o644); err != nil {
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
	if p.Name != "DNS over UDP" {
		t.Errorf("expected name=DNS over UDP, got %s", p.Name)
	}
	if p.Type != ProbeTypeUDP {
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

	p2 := probes[1]
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
