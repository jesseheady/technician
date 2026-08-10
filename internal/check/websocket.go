package check

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"golang.org/x/net/websocket"
)

type WebSocketChecker struct{}

func NewWebSocketChecker() *WebSocketChecker {
	return &WebSocketChecker{}
}

func (p *WebSocketChecker) Type() config.CheckType {
	return config.CheckTypeWebSocket
}

func (p *WebSocketChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypeWebSocket, origin)

	if cfg.WebSocket == nil {
		result.Error = "missing WebSocket check configuration"
		return result
	}

	wcfg := cfg.WebSocket
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// The WebSocket protocol requires an Origin; derive it from the target
	// scheme (wss→https, ws→http) so strict servers accept the handshake.
	u, err := url.Parse(wcfg.URL)
	if err != nil {
		result.Error = fmt.Sprintf("invalid WebSocket URL %q: %v", wcfg.URL, err)
		return result
	}
	originScheme := "http"
	if u.Scheme == "wss" {
		originScheme = "https"
	}

	wsCfg, err := websocket.NewConfig(wcfg.URL, originScheme+"://"+u.Host)
	if err != nil {
		result.Error = fmt.Sprintf("building WebSocket config: %v", err)
		return result
	}
	for k, v := range wcfg.Headers {
		wsCfg.Header.Set(k, v)
	}
	if wcfg.SkipTLS {
		// #nosec G402 -- opt-in for self-signed/internal endpoints
		wsCfg.TlsConfig = &tls.Config{InsecureSkipVerify: true}
	}
	wsCfg.Dialer = &net.Dialer{Timeout: timeout}

	start := time.Now()

	// DialConfig has no context variant, so run it in a goroutine and select on
	// ctx. Dialer.Timeout bounds the goroutine so it cannot outlive the check.
	type dialOut struct {
		conn *websocket.Conn
		err  error
	}
	ch := make(chan dialOut, 1)
	go func() {
		conn, err := websocket.DialConfig(wsCfg)
		ch <- dialOut{conn, err}
	}()

	var conn *websocket.Conn
	select {
	case <-ctx.Done():
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("connecting to %s: %v", wcfg.URL, ctx.Err())
		return result
	case out := <-ch:
		conn, err = out.conn, out.err
	}
	result.WSConnectDuration = time.Since(start)
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("connecting to %s: %v", wcfg.URL, err)
		return result
	}
	defer conn.Close()

	if wcfg.Send != "" {
		msgStart := time.Now()
		if err := websocket.Message.Send(conn, wcfg.Send); err != nil {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("sending to %s: %v", wcfg.URL, err)
			return result
		}

		if wcfg.ExpectRecv != "" {
			deadline := time.Now().Add(timeout)
			if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
				deadline = d
			}
			conn.SetReadDeadline(deadline)

			var reply string
			if err := websocket.Message.Receive(conn, &reply); err != nil {
				result.WSMessageDuration = time.Since(msgStart)
				result.Duration = time.Since(start)
				result.Error = fmt.Sprintf("reading from %s: %v", wcfg.URL, err)
				return result
			}
			result.WSMessageDuration = time.Since(msgStart)

			if !strings.Contains(reply, wcfg.ExpectRecv) {
				result.Duration = time.Since(start)
				result.Error = fmt.Sprintf("expected %q in response from %s, got %q", wcfg.ExpectRecv, wcfg.URL, reply)
				return result
			}
		} else {
			result.WSMessageDuration = time.Since(msgStart)
		}
	}

	result.Duration = time.Since(start)
	result.Success = true

	slog.Debug("WebSocket check completed",
		"name", cfg.Name,
		"url", wcfg.URL,
		"duration", result.Duration,
		"connect_duration", result.WSConnectDuration,
		"message_duration", result.WSMessageDuration,
	)

	return result
}
