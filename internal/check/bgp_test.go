package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestBGPCheckerType(t *testing.T) {
	checker := NewBGPChecker()
	if checker.Type() != config.CheckTypeBGP {
		t.Errorf("expected type %q, got %q", config.CheckTypeBGP, checker.Type())
	}
}

func TestBGPCheckerMissingConfig(t *testing.T) {
	checker := NewBGPChecker()
	cfg := &config.CheckConfig{
		Name: "test-bgp-nil",
		Type: config.CheckTypeBGP,
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil BGP config")
	}
	if result.Error != "missing BGP check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestBGPCheckerCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	checker := NewBGPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-bgp-cancelled",
		Type:    config.CheckTypeBGP,
		Timeout: 5 * time.Second,
		BGP: &config.BGPCheckConfig{
			Prefix:         "203.0.113.0/24",
			ExpectedOrigin: 64496,
		},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(ctx, cfg, origin)

	if result.Success {
		t.Error("expected failure for cancelled context")
	}
	if !result.InfraError {
		t.Error("expected InfraError=true for cancelled context")
	}
	if !strings.Contains(result.Error, "RIPE Stat query failed") {
		t.Errorf("expected error to mention RIPE Stat query failure, got: %s", result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

// bgpTestChecker returns a checker pointed at a stub RIPE Stat. The stub
// answers network-info with the given origin ASNs and rpki-validation with the
// given status. An empty rpkiStatus makes the RPKI endpoint fail, which the
// checker must treat as "unknown".
func bgpTestChecker(t *testing.T, asns, rpkiStatus string) *BGPChecker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "rpki-validation") {
			if got := r.URL.Query().Get("resource"); got != "AS64496" {
				t.Errorf("unexpected RPKI resource parameter: %q", got)
			}
			if rpkiStatus == "" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"status":"` + rpkiStatus + `"}}`))
			return
		}
		if got := r.URL.Query().Get("resource"); got != "203.0.113.0/24" {
			t.Errorf("unexpected resource parameter: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"asns":[` + asns + `],"prefix":"203.0.113.0/24"}}`))
	}))
	t.Cleanup(srv.Close)
	return &BGPChecker{baseURL: srv.URL}
}

func bgpTestConfig() *config.CheckConfig {
	return &config.CheckConfig{
		Name:    "test-bgp",
		Type:    config.CheckTypeBGP,
		Timeout: 5 * time.Second,
		BGP: &config.BGPCheckConfig{
			Prefix:         "203.0.113.0/24",
			ExpectedOrigin: 64496,
		},
	}
}

func TestBGPCheckerExpectedOrigin(t *testing.T) {
	result := bgpTestChecker(t, `"64496"`, "valid").Run(context.Background(), bgpTestConfig(), nil)

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if !result.BGPPrefixVisible || !result.BGPOriginMatch {
		t.Error("expected prefix visible and origin match")
	}
	if result.BGPOriginASN != 64496 {
		t.Errorf("expected origin AS64496, got AS%d", result.BGPOriginASN)
	}
}

// A hijacker announces the prefix while the real operator still announces it.
// RIPE Stat then lists both origins. The check must fail, whichever origin
// comes first in the list.
func TestBGPCheckerMultiOriginHijack(t *testing.T) {
	for _, tc := range []struct {
		name string
		asns string
	}{
		{"legitimate origin first", `"64496","64511"`},
		{"hijacker first", `"64511","64496"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := bgpTestChecker(t, tc.asns, "valid").Run(context.Background(), bgpTestConfig(), nil)

			if result.Success {
				t.Error("expected failure when an unexpected origin announces the prefix")
			}
			if result.BGPOriginMatch {
				t.Error("expected BGPOriginMatch=false")
			}
			if !result.BGPPrefixVisible {
				t.Error("expected BGPPrefixVisible=true — the prefix is announced")
			}
			if !strings.Contains(result.Error, "AS64511") {
				t.Errorf("expected error to name the unexpected origin, got: %s", result.Error)
			}
			if result.InfraError {
				t.Error("a hijack is a check failure, not an infrastructure error")
			}
		})
	}
}

func TestBGPCheckerPrefixNotVisible(t *testing.T) {
	result := bgpTestChecker(t, "", "valid").Run(context.Background(), bgpTestConfig(), nil)

	if result.Success || result.BGPPrefixVisible {
		t.Error("expected failure when the prefix is not announced")
	}
	if !strings.Contains(result.Error, "not visible") {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

// RPKI is checked after the origin comparison passes. An invalid verdict means
// no ROA authorizes the announcement, which is a hijack signal that does not
// depend on expected_origin being current.
func TestBGPCheckerRPKI(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rpkiStatus string
		wantOK     bool
		wantStatus string
	}{
		{"valid ROA", "valid", true, "valid"},
		{"origin not authorized", "invalid_asn", false, "invalid_asn"},
		{"prefix longer than ROA allows", "invalid_length", false, "invalid_length"},
		{"no ROA published", "unknown", true, "unknown"},
		{"RPKI query fails", "", true, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := bgpTestChecker(t, `"64496"`, tc.rpkiStatus).
				Run(context.Background(), bgpTestConfig(), nil)

			if result.Success != tc.wantOK {
				t.Errorf("Success = %v, want %v (error: %s)", result.Success, tc.wantOK, result.Error)
			}
			if result.BGPRPKIStatus != tc.wantStatus {
				t.Errorf("BGPRPKIStatus = %q, want %q", result.BGPRPKIStatus, tc.wantStatus)
			}
			// The origin matched in every case, so an RPKI failure must not
			// look like an origin mismatch.
			if !result.BGPOriginMatch {
				t.Error("expected BGPOriginMatch=true")
			}
			if result.InfraError {
				t.Error("expected InfraError=false")
			}
		})
	}
}

// A prefix that fails the origin comparison must not be reported as RPKI
// invalid, because the RPKI query never runs.
func TestBGPCheckerRPKISkippedOnOriginMismatch(t *testing.T) {
	result := bgpTestChecker(t, `"64511"`, "valid").
		Run(context.Background(), bgpTestConfig(), nil)

	if result.Success {
		t.Error("expected failure on origin mismatch")
	}
	if result.BGPRPKIStatus != "" {
		t.Errorf("expected no RPKI verdict, got %q", result.BGPRPKIStatus)
	}
}
