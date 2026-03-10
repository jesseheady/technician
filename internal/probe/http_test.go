package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
)

func TestHTTPProberSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
		},
	}
	site := &config.Site{Code: "test", City: "Test", Country: "XX"}

	result := prober.Run(context.Background(), cfg, site)

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
	if result.Labels["site_code"] != "test" {
		t.Errorf("expected site_code=test, got %s", result.Labels["site_code"])
	}
}

func TestHTTPProberUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-fail",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for status mismatch")
	}
	if result.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", result.StatusCode)
	}
}

func TestHTTPProberHeaders(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-headers",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Headers:        map[string]string{"Authorization": "Bearer token123"},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if receivedAuth != "Bearer token123" {
		t.Errorf("expected auth header, got %s", receivedAuth)
	}
}

func TestHTTPProberTimingFields(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello world"))
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-timing",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			SkipTLS:        true,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.TTFBDuration == 0 {
		t.Error("expected non-zero TTFB")
	}
}

func TestHTTPProberTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-timeout",
		Type:    config.ProbeTypeHTTP,
		Timeout: 100 * time.Millisecond,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure due to timeout")
	}
}

func TestHTTPProberMissingConfig(t *testing.T) {
	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name: "test-nil",
		Type: config.ProbeTypeHTTP,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil HTTP config")
	}
	if result.Error != "missing HTTP probe configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
