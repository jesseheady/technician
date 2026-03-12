package probe

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
)

func TestTCPProberSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	prober := NewTCPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-tcp-success",
		Type:    config.ProbeTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPProbeConfig{
			Host: "127.0.0.1",
			Port: addr.Port,
		},
	}
	site := &config.Site{Code: "test", City: "Test", Country: "XX"}

	result := prober.Run(context.Background(), cfg, site)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
	if result.TCPConnDuration == 0 {
		t.Error("expected non-zero TCPConnDuration")
	}
}

func TestTCPProberConnectionRefused(t *testing.T) {
	prober := NewTCPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-tcp-refused",
		Type:    config.ProbeTypeTCP,
		Timeout: 2 * time.Second,
		TCP: &config.TCPProbeConfig{
			Host: "127.0.0.1",
			Port: 1,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for connection refused")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestTCPProberSendExpect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				c.Write(buf[:n])
			}(conn)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	prober := NewTCPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-tcp-send-expect",
		Type:    config.ProbeTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPProbeConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			Send:       "PING\n",
			ExpectRecv: "PING",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestTCPProberExpectFail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.Write([]byte("WRONG"))
			}(conn)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	prober := NewTCPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-tcp-expect-fail",
		Type:    config.ProbeTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPProbeConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			ExpectRecv: "CORRECT",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for mismatched expect recv")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestTCPProberMissingConfig(t *testing.T) {
	prober := NewTCPProber()
	cfg := &config.ProbeConfig{
		Name: "test-tcp-nil",
		Type: config.ProbeTypeTCP,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil TCP config")
	}
	if result.Error != "missing TCP probe configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestTCPProberIPv4(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start tcp4 listener: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	prober := NewTCPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-tcp-ipv4",
		Type:    config.ProbeTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPProbeConfig{
			Host:      "127.0.0.1",
			Port:      addr.Port,
			IPVersion: "4",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}
