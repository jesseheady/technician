package probe

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
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

func TestUDPProberSuccess(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-success",
		Type:    config.ProbeTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host: "127.0.0.1",
			Port: addr.Port,
			Send: "PING",
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
	if result.UDPRTT == 0 {
		t.Error("expected non-zero UDPRTT")
	}
	if result.UDPResponseBytes != 4 {
		t.Errorf("expected 4 response bytes, got %d", result.UDPResponseBytes)
	}
}

func TestUDPProberExpectRecv(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-expect",
		Type:    config.ProbeTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			Send:       "HELLO",
			ExpectRecv: "HELLO",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestUDPProberExpectRecvFail(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-expect-fail",
		Type:    config.ProbeTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host:       "127.0.0.1",
			Port:       addr.Port,
			Send:       "WRONG",
			ExpectRecv: "CORRECT",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for mismatched expect_recv")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestUDPProberNoResponseExpected(t *testing.T) {
	// Send to a port with no listener. With expect_response=false,
	// fire-and-forget should succeed.
	prober := NewUDPProber()
	expectResponse := false
	cfg := &config.ProbeConfig{
		Name:    "test-udp-no-response",
		Type:    config.ProbeTypeUDP,
		Timeout: 2 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host:           "127.0.0.1",
			Port:           19999,
			Send:           "test.metric:1|c",
			ExpectResponse: &expectResponse,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success for fire-and-forget, got error: %s", result.Error)
	}
}

func TestUDPProberTimeoutExpectingResponse(t *testing.T) {
	// Send to a port with no listener, expecting a response — should timeout.
	// Use a high port that's unlikely to have a listener.
	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-timeout",
		Type:    config.ProbeTypeUDP,
		Timeout: 1 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host: "127.0.0.1",
			Port: 19998,
			Send: "PING",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure when no response and expect_response is true")
	}
}

func TestUDPProberSendHex(t *testing.T) {
	addr, cleanup := startUDPEcho(t)
	defer cleanup()

	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-hex",
		Type:    config.ProbeTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host:    "127.0.0.1",
			Port:    addr.Port,
			SendHex: "48454c4c4f", // "HELLO" in hex
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.UDPResponseBytes != 5 {
		t.Errorf("expected 5 response bytes, got %d", result.UDPResponseBytes)
	}
}

func TestUDPProberInvalidHex(t *testing.T) {
	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-bad-hex",
		Type:    config.ProbeTypeUDP,
		Timeout: 2 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host:    "127.0.0.1",
			Port:    1234,
			SendHex: "ZZZZ",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for invalid hex")
	}
}

func TestUDPProberMissingConfig(t *testing.T) {
	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name: "test-udp-nil",
		Type: config.ProbeTypeUDP,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil UDP config")
	}
	if result.Error != "missing UDP probe configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestUDPProberMissingPayload(t *testing.T) {
	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-no-payload",
		Type:    config.ProbeTypeUDP,
		Timeout: 2 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host: "127.0.0.1",
			Port: 1234,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for missing payload")
	}
	if result.Error != "either send or send_hex must be specified" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestUDPProberIPv4(t *testing.T) {
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

	prober := NewUDPProber()
	cfg := &config.ProbeConfig{
		Name:    "test-udp-ipv4",
		Type:    config.ProbeTypeUDP,
		Timeout: 5 * time.Second,
		UDP: &config.UDPProbeConfig{
			Host:      "127.0.0.1",
			Port:      addr.Port,
			IPVersion: "4",
			Send:      "PING",
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}
