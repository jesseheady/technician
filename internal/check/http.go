package check

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

type httpClientKey struct {
	skipTLS         bool
	followRedirects bool
	ipVersion       string
}

type HTTPChecker struct {
	mu      sync.Mutex
	clients map[httpClientKey]*http.Client
}

func NewHTTPChecker() *HTTPChecker {
	return &HTTPChecker{
		clients: make(map[httpClientKey]*http.Client),
	}
}

// getClient returns a shared *http.Client for the given config, reusing
// transports so TCP/TLS connections are pooled across check runs.
func (p *HTTPChecker) getClient(skipTLS, followRedirects bool, ipVersion string) *http.Client {
	key := httpClientKey{skipTLS: skipTLS, followRedirects: followRedirects, ipVersion: ipVersion}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[key]; ok {
		return c
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipTLS,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
	}
	// Force the address family when ip_version is set; the dialer honors
	// the tcp4/tcp6 network and ignores the network passed by the transport.
	if network := ipVersionNetwork(ipVersion); network != "tcp" {
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		transport.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}
	}
	client := &http.Client{Transport: transport}
	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	p.clients[key] = client
	return client
}

// ipVersionNetwork maps an ip_version config value to a dial network.
// "4" and "6" force IPv4/IPv6; anything else allows either family.
func ipVersionNetwork(ipVersion string) string {
	switch ipVersion {
	case "4":
		return "tcp4"
	case "6":
		return "tcp6"
	default:
		return "tcp"
	}
}

func (p *HTTPChecker) Type() config.CheckType {
	return config.CheckTypeHTTP
}

func (p *HTTPChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypeHTTP, origin)

	if cfg.HTTP == nil {
		result.Error = "missing HTTP check configuration"
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

	client := p.getClient(hcfg.SkipTLS, hcfg.FollowRedirects, hcfg.IPVersion)

	reqStart = time.Now()
	resp, err := client.Do(req)
	totalDuration := time.Since(reqStart)

	if err != nil {
		result.Duration = totalDuration
		result.Error = fmt.Sprintf("request failed: %v", err)
		slog.Warn("HTTP check failed", "name", cfg.Name, "url", hcfg.URL, "error", err)
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

	slog.Debug("HTTP check completed",
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

// regexCache caches compiled regular expressions used in assertions to avoid
// recompiling the same pattern on every check run.
var regexCache sync.Map // map[string]*regexp.Regexp

func cachedCompile(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, re)
	return re, nil
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
		re, err := cachedCompile(a.Target)
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
		re, err := cachedCompile(a.Target)
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
