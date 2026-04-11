package check

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

// startUDPEcho starts a UDP listener that echoes back whatever it receives.
// Returns the address and a cleanup function.
func startUDPEcho(t *testing.T) (*net.UDPAddr, func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start UDP listener: %v", err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, remote, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			conn.WriteTo(buf[:n], remote)
		}
	}()

	return addr, func() { conn.Close() }
}

func TestUDPCheckerSuccess(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-success",
		Type:    config.CheckTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host: "127.0.0.1",
			Port: addr.Port,
			Send: "PING",
		},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(context.Background(), cfg, origin)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
	if result.UDPRTT == 0 {
		t.Error("expected non-zero UDPRTT")
	}
	if result.UDPResponseBytes != 4 {
		t.Errorf("expected 4 response bytes, got %d", result.UDPResponseBytes)
	}
}

func TestUDPCheckerExpectRecv(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-expect",
		Type:    config.CheckTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			Send:       "HELLO",
			ExpectRecv: "HELLO",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestUDPCheckerExpectRecvFail(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-expect-fail",
		Type:    config.CheckTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			Send:       "WRONG",
			ExpectRecv: "CORRECT",
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

func TestUDPCheckerNoResponseExpected(t *testing.T) {
	// Send to a port with no listener. With expect_response=false,
	// fire-and-forget should succeed.
	checker := NewUDPChecker()
	expectResponse := false
	cfg := &config.CheckConfig{
		Name:    "test-udp-no-response",
		Type:    config.CheckTypeUDP,
		Timeout: 2 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host:           "127.0.0.1",
			Port:           19999,
			Send:           "test.metric:1|c",
			ExpectResponse: &expectResponse,
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success for fire-and-forget, got error: %s", result.Error)
	}
}

func TestUDPCheckerTimeoutExpectingResponse(t *testing.T) {
	// Send to a port with no listener, expecting a response — should timeout.
	// Use a high port that's unlikely to have a listener.
	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-timeout",
		Type:    config.CheckTypeUDP,
		Timeout: 1 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host: "127.0.0.1",
			Port: 19998,
			Send: "PING",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure when no response and expect_response is true")
	}
}

func TestUDPCheckerSendHex(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-hex",
		Type:    config.CheckTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host:    "127.0.0.1",
			Port:    addr.Port,
			SendHex: "48454c4c4f", // "HELLO" in hex
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.UDPResponseBytes != 5 {
		t.Errorf("expected 5 response bytes, got %d", result.UDPResponseBytes)
	}
}

func TestUDPCheckerInvalidHex(t *testing.T) {
	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-bad-hex",
		Type:    config.CheckTypeUDP,
		Timeout: 2 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host:    "127.0.0.1",
			Port:    1234,
			SendHex: "ZZZZ",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for invalid hex")
	}
}

func TestUDPCheckerMissingConfig(t *testing.T) {
	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name: "test-udp-nil",
		Type: config.CheckTypeUDP,
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil UDP config")
	}
	if result.Error != "missing UDP check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestUDPCheckerMissingPayload(t *testing.T) {
	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-no-payload",
		Type:    config.CheckTypeUDP,
		Timeout: 2 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host: "127.0.0.1",
			Port: 1234,
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for missing payload")
	}
	if result.Error != "either send or send_hex must be specified" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestUDPCheckerIPv4(t *testing.T) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start udp4 listener: %v", err)
	}
	defer conn.Close()
	addr := conn.LocalAddr().(*net.UDPAddr)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, remote, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			conn.WriteTo(buf[:n], remote)
		}
	}()

	checker := NewUDPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-udp-ipv4",
		Type:    config.CheckTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPCheckConfig{
			Host:      "127.0.0.1",
			Port:      addr.Port,
			IPVersion: "4",
			Send:      "PING",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}
