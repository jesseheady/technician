package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

func TestHTTPProberHeaderContains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "hello-world")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-header-contains",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "header_contains", Header: "X-Custom", Target: "hello"},
			},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

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

func TestHTTPProberHeaderContainsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "nope")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-header-contains-fail",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "header_contains", Header: "X-Custom", Target: "hello"},
			},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for header_contains mismatch")
	}
	if len(result.Assertions) != 1 || result.Assertions[0].Passed {
		t.Error("expected assertion to fail")
	}
}

func TestHTTPProberHeaderNotContains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "production")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-header-not-contains",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "header_not_contains", Header: "X-Custom", Target: "staging"},
			},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

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

func TestHTTPProberHeaderRegex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", "v2.5.1")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-header-regex",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "header_regex", Header: "X-Version", Target: `v\d+\.\d+\.\d+`},
			},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

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

func TestHTTPProberHeaderRegexFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", "latest")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-header-regex-fail",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:            server.URL,
			Method:         "GET",
			ExpectedStatus: 200,
			Assertions: []config.Assertion{
				{Type: "header_regex", Header: "X-Version", Target: `v\d+\.\d+`},
			},
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for header_regex mismatch")
	}
	if len(result.Assertions) != 1 || result.Assertions[0].Passed {
		t.Error("expected assertion to fail")
	}
}

func TestHTTPProberFollowRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final destination"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-follow-redirects",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:             server.URL,
			Method:          "GET",
			ExpectedStatus:  200,
			FollowRedirects: true,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success with follow_redirects=true, got error: %s", result.Error)
	}
	if result.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
}

func TestHTTPProberNoFollowRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/target", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("final destination"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	prober := NewHTTPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-no-follow-redirects",
		Type:    config.ProbeTypeHTTP,
		Timeout: 5 * time.Second,
		HTTP: &config.HTTPProbeConfig{
			URL:             server.URL,
			Method:          "GET",
			ExpectedStatus:  301,
			FollowRedirects: false,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success with follow_redirects=false and expected_status=301, got error: %s", result.Error)
	}
	if result.StatusCode != 301 {
		t.Errorf("expected status 301, got %d", result.StatusCode)
	}
}
