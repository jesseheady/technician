package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strings"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
)

type HTTPProber struct{}

func NewHTTPProber() *HTTPProber {
	return &HTTPProber{}
}

func (p *HTTPProber) Type() config.ProbeType {
	return config.ProbeTypeHTTP
}

func (p *HTTPProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypeHTTP, site)

	if cfg.HTTP == nil {
		result.Error = "missing HTTP probe configuration"
		return result
	}

	hcfg := cfg.HTTP

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		dnsStart     time.Time
		dnsDone      time.Time
		connectStart time.Time
		connectDone  time.Time
		tlsStart     time.Time
		tlsDone      time.Time
		gotFirstByte time.Time
		reqStart     time.Time
	)

	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			dnsDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, err error) {
			connectDone = time.Now()
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			tlsDone = time.Now()
		},
		GotFirstResponseByte: func() {
			gotFirstByte = time.Now()
		},
	}

	var body io.Reader
	if hcfg.Body != "" {
		body = strings.NewReader(hcfg.Body)
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), hcfg.Method, hcfg.URL, body)
	if err != nil {
		result.Error = fmt.Sprintf("creating request: %v", err)
		return result
	}

	for k, v := range hcfg.Headers {
		req.Header.Set(k, v)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: hcfg.SkipTLS,
		},
	}
	client := &http.Client{
		Transport: transport,
	}
	if !hcfg.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	reqStart = time.Now()
	resp, err := client.Do(req)
	totalDuration := time.Since(reqStart)

	if err != nil {
		result.Duration = totalDuration
		result.Error = fmt.Sprintf("request failed: %v", err)
		slog.Warn("HTTP probe failed", "name", cfg.Name, "url", hcfg.URL, "error", err)
		return result
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	transferDone := time.Now()

	if err != nil {
		result.Duration = totalDuration
		result.Error = fmt.Sprintf("reading response body: %v", err)
		return result
	}

	result.StatusCode = resp.StatusCode
	result.ResponseBytes = int64(len(respBody))
	result.Duration = totalDuration

	if !dnsStart.IsZero() && !dnsDone.IsZero() {
		result.DNSDuration = dnsDone.Sub(dnsStart)
	}
	if !connectStart.IsZero() && !connectDone.IsZero() {
		result.ConnDuration = connectDone.Sub(connectStart)
	}
	if !tlsStart.IsZero() && !tlsDone.IsZero() {
		result.TLSDuration = tlsDone.Sub(tlsStart)
	}
	if !gotFirstByte.IsZero() {
		result.TTFBDuration = gotFirstByte.Sub(reqStart)
	}
	if !gotFirstByte.IsZero() {
		result.TransferDuration = transferDone.Sub(gotFirstByte)
	}

	result.Success = resp.StatusCode == hcfg.ExpectedStatus

	if !result.Success {
		result.Error = fmt.Sprintf("expected status %d, got %d", hcfg.ExpectedStatus, resp.StatusCode)
	}

	// Evaluate assertions (body + header)
	if len(hcfg.Assertions) > 0 {
		bodyStr := string(respBody)
		for _, a := range hcfg.Assertions {
			var ar AssertionResult
			if strings.HasPrefix(a.Type, "header_") {
				ar = evaluateHeaderAssertion(a, resp.Header)
			} else {
				ar = evaluateAssertion(a, bodyStr)
			}
			result.Assertions = append(result.Assertions, ar)
			if !ar.Passed && result.Success {
				result.Success = false
				result.Error = fmt.Sprintf("assertion failed: %s", ar.Message)
			}
		}
	}

	slog.Debug("HTTP probe completed",
		"name", cfg.Name,
		"url", hcfg.URL,
		"status", resp.StatusCode,
		"duration", totalDuration,
		"dns", result.DNSDuration,
		"tls", result.TLSDuration,
		"ttfb", result.TTFBDuration,
		"success", result.Success,
	)

	return result
}

func evaluateHeaderAssertion(a config.Assertion, headers http.Header) AssertionResult {
	ar := AssertionResult{Type: a.Type, Target: a.Target, Passed: true}
	headerVal := headers.Get(a.Header)
	switch a.Type {
	case "header_contains":
		if !strings.Contains(headerVal, a.Target) {
			ar.Passed = false
			ar.Message = fmt.Sprintf("header %q does not contain %q (got %q)", a.Header, a.Target, headerVal)
		}
	case "header_not_contains":
		if strings.Contains(headerVal, a.Target) {
			ar.Passed = false
			ar.Message = fmt.Sprintf("header %q contains %q", a.Header, a.Target)
		}
	case "header_regex":
		re, err := regexp.Compile(a.Target)
		if err != nil {
			ar.Passed = false
			ar.Message = fmt.Sprintf("invalid regex %q: %v", a.Target, err)
		} else if !re.MatchString(headerVal) {
			ar.Passed = false
			ar.Message = fmt.Sprintf("header %q does not match regex %q (got %q)", a.Header, a.Target, headerVal)
		}
	default:
		ar.Passed = false
		ar.Message = fmt.Sprintf("unknown header assertion type %q", a.Type)
	}
	return ar
}

func evaluateAssertion(a config.Assertion, body string) AssertionResult {
	ar := AssertionResult{Type: a.Type, Target: a.Target, Passed: true}
	switch a.Type {
	case "contains":
		if !strings.Contains(body, a.Target) {
			ar.Passed = false
			ar.Message = fmt.Sprintf("body does not contain %q", a.Target)
		}
	case "not_contains":
		if strings.Contains(body, a.Target) {
			ar.Passed = false
			ar.Message = fmt.Sprintf("body contains %q", a.Target)
		}
	case "regex":
		re, err := regexp.Compile(a.Target)
		if err != nil {
			ar.Passed = false
			ar.Message = fmt.Sprintf("invalid regex %q: %v", a.Target, err)
		} else if !re.MatchString(body) {
			ar.Passed = false
			ar.Message = fmt.Sprintf("body does not match regex %q", a.Target)
		}
	default:
		ar.Passed = false
		ar.Message = fmt.Sprintf("unknown assertion type %q", a.Type)
	}
	return ar
}
