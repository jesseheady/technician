package check

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"golang.org/x/net/websocket"
)

// startWSEcho starts a WebSocket echo server. If tls is true it serves over
// wss. Returns the ws(s):// URL and a cleanup func.
func startWSEcho(t *testing.T, useTLS bool) (string, func()) {
	t.Helper()
	h := websocket.Handler(func(ws *websocket.Conn) {
		var msg string
		for {
			if err := websocket.Message.Receive(ws, &msg); err != nil {
				return
			}
			if err := websocket.Message.Send(ws, msg); err != nil {
				return
			}
		}
	})
	if useTLS {
		srv := httptest.NewTLSServer(h)
		return "wss" + strings.TrimPrefix(srv.URL, "https"), srv.Close
	}
	srv := httptest.NewServer(h)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), srv.Close
}

func TestWebSocketCheckerConnectOnly(t *testing.T) {
	url, cleanup := startWSEcho(t, false)
	defer cleanup()

	checker := NewWebSocketChecker()
	cfg := &config.CheckConfig{
		Name:      "test-ws-connect",
		Type:      config.CheckTypeWebSocket,
		Timeout:   5 * time.Second,
		WebSocket: &config.WebSocketCheckConfig{URL: url},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(context.Background(), cfg, origin)

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.WSConnectDuration == 0 {
		t.Error("expected non-zero connect duration")
	}
}

func TestWebSocketCheckerEcho(t *testing.T) {
	url, cleanup := startWSEcho(t, false)
	defer cleanup()

	checker := NewWebSocketChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ws-echo",
		Type:    config.CheckTypeWebSocket,
		Timeout: 5 * time.Second,
		WebSocket: &config.WebSocketCheckConfig{
			URL:        url,
			Send:       "ping",
			ExpectRecv: "ping",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.WSMessageDuration == 0 {
		t.Error("expected non-zero message duration")
	}
}

func TestWebSocketCheckerExpectMismatch(t *testing.T) {
	url, cleanup := startWSEcho(t, false)
	defer cleanup()

	checker := NewWebSocketChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ws-mismatch",
		Type:    config.CheckTypeWebSocket,
		Timeout: 5 * time.Second,
		WebSocket: &config.WebSocketCheckConfig{
			URL:        url,
			Send:       "ping",
			ExpectRecv: "pong", // echo returns "ping"
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for mismatched expect_recv")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestWebSocketCheckerTLSSkipVerify(t *testing.T) {
	url, cleanup := startWSEcho(t, true)
	defer cleanup()

	checker := NewWebSocketChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ws-tls",
		Type:    config.CheckTypeWebSocket,
		Timeout: 5 * time.Second,
		WebSocket: &config.WebSocketCheckConfig{
			URL:        url,
			SkipTLS:    true, // httptest TLS server uses a self-signed cert
			Send:       "secure",
			ExpectRecv: "secure",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Fatalf("expected success over wss with skip_tls, got error: %s", result.Error)
	}
}

func TestWebSocketCheckerUnreachable(t *testing.T) {
	checker := NewWebSocketChecker()
	cfg := &config.CheckConfig{
		Name:      "test-ws-unreachable",
		Type:      config.CheckTypeWebSocket,
		Timeout:   1 * time.Second,
		WebSocket: &config.WebSocketCheckConfig{URL: "ws://127.0.0.1:1"},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for unreachable endpoint")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestWebSocketCheckerMissingConfig(t *testing.T) {
	checker := NewWebSocketChecker()
	cfg := &config.CheckConfig{Name: "test-ws-nil", Type: config.CheckTypeWebSocket}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil WebSocket config")
	}
	if result.Error != "missing WebSocket check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
