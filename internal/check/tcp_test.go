package check

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func TestTCPCheckerSuccess(t *testing.T) {
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
	checker := NewTCPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tcp-success",
		Type:    config.CheckTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPCheckConfig{
			Host: "127.0.0.1",
			Port: addr.Port,
		},
	}
	site := &config.Site{Code: "test", City: "Test", Country: "XX"}

	result := checker.Run(context.Background(), cfg, site)

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

func TestTCPCheckerConnectionRefused(t *testing.T) {
	checker := NewTCPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tcp-refused",
		Type:    config.CheckTypeTCP,
		Timeout: 2 * time.Second,
		TCP: &config.TCPCheckConfig{
			Host: "127.0.0.1",
			Port: 1,
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for connection refused")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestTCPCheckerSendExpect(t *testing.T) {
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
	checker := NewTCPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tcp-send-expect",
		Type:    config.CheckTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPCheckConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			Send:       "PING\n",
			ExpectRecv: "PING",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestTCPCheckerExpectFail(t *testing.T) {
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
	checker := NewTCPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tcp-expect-fail",
		Type:    config.CheckTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPCheckConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			ExpectRecv: "CORRECT",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for mismatched expect recv")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestTCPCheckerMissingConfig(t *testing.T) {
	checker := NewTCPChecker()
	cfg := &config.CheckConfig{
		Name: "test-tcp-nil",
		Type: config.CheckTypeTCP,
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil TCP config")
	}
	if result.Error != "missing TCP check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestTCPCheckerIPv4(t *testing.T) {
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
	checker := NewTCPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tcp-ipv4",
		Type:    config.CheckTypeTCP,
		Timeout: 5 * time.Second,
		TCP: &config.TCPCheckConfig{
			Host:      "127.0.0.1",
			Port:      addr.Port,
			IPVersion: "4",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}
