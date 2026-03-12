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
