package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
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
