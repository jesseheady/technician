package check

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestHTTPCheckerSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
		},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(context.Background(), cfg, origin)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
	if result.ResponseBytes != 2 {
		t.Errorf("expected 2 response bytes, got %d", result.ResponseBytes)
	}
	if result.Labels["region"] != "test" {
		t.Errorf("expected region=test, got %s", result.Labels["region"])
	}
}

func TestHTTPCheckerIPVersion(t *testing.T) {
	// httptest serves on 127.0.0.1, so ip_version "4" must succeed and
	// "6" must fail to connect because the IPv4 address can't be reached
	// over a tcp6-forced dial.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	t.Run("force IPv4 succeeds", func(t *testing.T) {
		checker := NewHTTPChecker()
		cfg := &config.CheckConfig{
			Name:    "ipv4",
			Type:    config.CheckTypeHTTP,
			Timeout: 5 * time.Second,
			HTTP: &config.HTTPCheckConfig{
				URL:            server.URL,
				Method:         "GET",
				ExpectedStatus: 200,
				IPVersion:      "4",
			},
		}
		if result := checker.Run(context.Background(), cfg, origin); !result.Success {
			t.Errorf("expected success forcing IPv4, got error: %s", result.Error)
		}
	})

	t.Run("force IPv6 fails for IPv4 target", func(t *testing.T) {
		checker := NewHTTPChecker()
		cfg := &config.CheckConfig{
			Name:    "ipv6",
			Type:    config.CheckTypeHTTP,
			Timeout: 5 * time.Second,
			HTTP: &config.HTTPCheckConfig{
				URL:            server.URL,
				Method:         "GET",
				ExpectedStatus: 200,
				IPVersion:      "6",
			},
		}
		if result := checker.Run(context.Background(), cfg, origin); result.Success {
			t.Error("expected failure forcing IPv6 against an IPv4-only target")
		}
	})
}

func TestHTTPCheckerMinTLS(t *testing.T) {
	// Server caps at TLS 1.2. A client requiring min 1.2 succeeds; a client
	// requiring min 1.3 fails the handshake.
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{MaxVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()

	t.Run("min_tls satisfied", func(t *testing.T) {
		checker := NewHTTPChecker()
		cfg := &config.CheckConfig{
			Name:    "tls-ok",
			Type:    config.CheckTypeHTTP,
			Timeout: 5 * time.Second,
			HTTP: &config.HTTPCheckConfig{
				URL:            server.URL,
				Method:         "GET",
				ExpectedStatus: 200,
				SkipTLS:        true, // self-signed test cert
				MinTLS:         "1.2",
			},
		}
		if result := checker.Run(context.Background(), cfg, nil); !result.Success {
			t.Errorf("expected success with min_tls 1.2, got error: %s", result.Error)
		}
	})

	t.Run("min_tls violated", func(t *testing.T) {
		checker := NewHTTPChecker()
		cfg := &config.CheckConfig{
			Name:    "tls-fail",
			Type:    config.CheckTypeHTTP,
			Timeout: 5 * time.Second,
			HTTP: &config.HTTPCheckConfig{
				URL:            server.URL,
				Method:         "GET",
				ExpectedStatus: 200,
				SkipTLS:        true,
				MinTLS:         "1.3",
			},
		}
		if result := checker.Run(context.Background(), cfg, nil); result.Success {
			t.Error("expected handshake failure when min_tls 1.3 exceeds server max 1.2")
		}
	})
}

func TestHTTPCheckerUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-fail",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for status mismatch")
	}
	if result.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", result.StatusCode)
	}
}

func TestHTTPCheckerHeaders(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-headers",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Headers:        map[string]string{"Authorization": "Bearer token123"},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if receivedAuth != "Bearer token123" {
		t.Errorf("expected auth header, got %s", receivedAuth)
	}
}

func TestHTTPCheckerBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	var gotOK bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-basic-auth",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			BasicAuth:      &config.BasicAuth{Username: "alice", Password: "s3cret"},
		},
	}

	if result := checker.Run(context.Background(), cfg, nil); !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if !gotOK || gotUser != "alice" || gotPass != "s3cret" {
		t.Errorf("expected basic auth alice/s3cret, got ok=%v user=%q pass=%q", gotOK, gotUser, gotPass)
	}
}

func TestHTTPCheckerBearerToken(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-bearer",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			BearerToken:    "abc.def.ghi",
		},
	}

	if result := checker.Run(context.Background(), cfg, nil); !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if receivedAuth != "Bearer abc.def.ghi" {
		t.Errorf("expected bearer auth header, got %q", receivedAuth)
	}
}

func TestHTTPCheckerTimingFields(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello world"))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-timing",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			SkipTLS:        true,
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.TTFBDuration == 0 {
		t.Error("expected non-zero TTFB")
	}
}

func TestHTTPCheckerTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-timeout",
		Type:    config.CheckTypeHTTP,
		Timeout: 100 * time.Millisecond,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure due to timeout")
	}
}

func TestHTTPCheckerAssertionContains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","version":"1.2.3"}`))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-assert",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "contains", Target: `"status":"ok"`},
			},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if len(result.Assertions) != 1 {
		t.Fatalf("expected 1 assertion result, got %d", len(result.Assertions))
	}
	if !result.Assertions[0].Passed {
		t.Errorf("expected assertion to pass: %s", result.Assertions[0].Message)
	}
}

func TestHTTPCheckerAssertionContainsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"error"}`))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-assert-fail",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "contains", Target: `"status":"ok"`},
			},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)
	if result.Success {
		t.Error("expected failure for missing body content")
	}
	if len(result.Assertions) != 1 || result.Assertions[0].Passed {
		t.Error("expected assertion to fail")
	}
}

func TestHTTPCheckerAssertionNotContains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-not-contains",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "not_contains", Target: "error"},
			},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestHTTPCheckerAssertionRegex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version":"2.5.1"}`))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-regex",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "regex", Target: `"version":"\d+\.\d+\.\d+"`},
			},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if !result.Assertions[0].Passed {
		t.Errorf("expected regex assertion to pass: %s", result.Assertions[0].Message)
	}
}

func TestHTTPCheckerAssertionStatusOverride(t *testing.T) {
	// Status code matches but assertion fails — check should fail
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("maintenance mode"))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-assert-override",
		Type:    config.CheckTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPCheckConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "not_contains", Target: "maintenance"},
			},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)
	if result.Success {
		t.Error("expected failure — status 200 but assertion should fail")
	}
}

func TestHTTPCheckerMissingConfig(t *testing.T) {
	checker := NewHTTPChecker()
	cfg := &config.CheckConfig{
		Name: "test-nil",
		Type: config.CheckTypeHTTP,
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil HTTP config")
	}
	if result.Error != "missing HTTP check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
