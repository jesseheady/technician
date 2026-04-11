package check

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

type TCPChecker struct{}

func NewTCPChecker() *TCPChecker {
	return &TCPChecker{}
}

func (p *TCPChecker) Type() config.CheckType {
	return config.CheckTypeTCP
}

func (p *TCPChecker) Run(ctx context.Context, cfg *config.CheckConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.CheckTypeTCP, site)

	if cfg.TCP == nil {
		result.Error = "missing TCP check configuration"
		return result
	}

	tcfg := cfg.TCP
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", tcfg.Host, tcfg.Port)

	network := "tcp"
	switch tcfg.IPVersion {
	case "4":
		network = "tcp4"
	case "6":
		network = "tcp6"
	}

	start := time.Now()

	dialer := net.Dialer{Timeout: timeout}
	connStart := time.Now()
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("connecting to %s: %v", addr, err)
		return result
	}
	result.TCPConnDuration = time.Since(connStart)
	defer conn.Close()

	if tcfg.TLS {
		tlsStart := time.Now()
		tlsConn := tls.Client(conn, &tls.Config{
			ServerName: tcfg.Host,
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			result.TCPTLSDuration = time.Since(tlsStart)
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("TLS handshake with %s: %v", addr, err)
			return result
		}
		result.TCPTLSDuration = time.Since(tlsStart)
		// Replace conn so subsequent reads/writes go through TLS
		conn = tlsConn
	}

	if tcfg.Send != "" {
		if _, err := conn.Write([]byte(tcfg.Send)); err != nil {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("sending data to %s: %v", addr, err)
			return result
		}
	}

	if tcfg.ExpectRecv != "" {
		// Set a read deadline based on remaining timeout
		elapsed := time.Since(start)
		remaining := timeout - elapsed
		if remaining <= 0 {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("timeout exceeded before reading from %s", addr)
			return result
		}
		conn.SetReadDeadline(time.Now().Add(remaining))

		const maxRecvSize = 1 << 20 // 1 MB limit
		buf := make([]byte, 4096)
		var received strings.Builder
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				received.Write(buf[:n])
			}
			if strings.Contains(received.String(), tcfg.ExpectRecv) {
				break
			}
			if received.Len() > maxRecvSize {
				result.Duration = time.Since(start)
				result.Error = fmt.Sprintf("response from %s exceeded %d bytes without matching expected string", addr, maxRecvSize)
				return result
			}
			if err != nil {
				if err == io.EOF {
					break
				}
				result.Duration = time.Since(start)
				result.Error = fmt.Sprintf("reading from %s: %v", addr, err)
				return result
			}
		}

		if !strings.Contains(received.String(), tcfg.ExpectRecv) {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("expected %q in response from %s, got %q", tcfg.ExpectRecv, addr, received.String())
			return result
		}
	}

	result.Duration = time.Since(start)
	result.Success = true

	slog.Debug("TCP check completed",
		"name", cfg.Name,
		"host", addr,
		"duration", result.Duration,
		"conn_duration", result.TCPConnDuration,
		"tls_duration", result.TCPTLSDuration,
	)

	return result
}
