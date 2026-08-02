package check

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

// encodeNTPTime encodes a wall-clock time into NTP's (seconds, fraction) pair.
func encodeNTPTime(t time.Time) (sec, frac uint32) {
	sec = uint32(t.Unix() + ntpEpochOffset)
	frac = uint32((int64(t.Nanosecond()) << 32) / 1e9)
	return
}

// startNTPServer starts a UDP server that replies to each datagram with the
// bytes returned by respond. A nil return sends nothing (simulating a dead
// server). Returns the host and port to point a check at, plus a cleanup func.
func startNTPServer(t *testing.T, respond func(req []byte) []byte) (string, int, func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start NTP server: %v", err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr)

	go func() {
		buf := make([]byte, 48)
		for {
			n, remote, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if resp := respond(buf[:n]); resp != nil {
				conn.WriteTo(resp, remote)
			}
		}
	}()

	return "127.0.0.1", addr.Port, func() { conn.Close() }
}

// validNTPResponse builds a well-formed server reply with the given leap
// indicator and stratum, stamping the receive/transmit fields with now.
func validNTPResponse(li, stratum uint8) []byte {
	resp := make([]byte, 48)
	resp[0] = (li << 6) | (4 << 3) | 4 // LI | VN=4 | Mode=4 (server)
	resp[1] = stratum
	sec, frac := encodeNTPTime(time.Now())
	binary.BigEndian.PutUint32(resp[32:36], sec) // Rx
	binary.BigEndian.PutUint32(resp[36:40], frac)
	binary.BigEndian.PutUint32(resp[40:44], sec) // Tx
	binary.BigEndian.PutUint32(resp[44:48], frac)
	return resp
}

func TestNTPTimeToTime(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	sec, frac := encodeNTPTime(now)
	got := ntpTimeToTime(sec, frac)
	if diff := got.Sub(now); diff < -time.Millisecond || diff > time.Millisecond {
		t.Errorf("round-trip drift too large: %v", diff)
	}
}

func TestNTPCheckerSuccess(t *testing.T) {
	host, port, cleanup := startNTPServer(t, func([]byte) []byte {
		return validNTPResponse(0, 2)
	})
	defer cleanup()

	checker := NewNTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ntp-success",
		Type:    config.CheckTypeNTP,
		Timeout: 5 * time.Second,
		NTP:     &config.NTPCheckConfig{Server: host, Port: port},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(context.Background(), cfg, origin)

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.NTPStratum != 2 {
		t.Errorf("expected stratum 2, got %d", result.NTPStratum)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestNTPCheckerUnsynchronized(t *testing.T) {
	host, port, cleanup := startNTPServer(t, func([]byte) []byte {
		return validNTPResponse(3, 2) // LI=3 → unsynchronized
	})
	defer cleanup()

	checker := NewNTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ntp-li3",
		Type:    config.CheckTypeNTP,
		Timeout: 5 * time.Second,
		NTP:     &config.NTPCheckConfig{Server: host, Port: port},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for LI=3")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestNTPCheckerKissOfDeath(t *testing.T) {
	host, port, cleanup := startNTPServer(t, func([]byte) []byte {
		return validNTPResponse(0, 0) // stratum 0 → kiss-o'-death
	})
	defer cleanup()

	checker := NewNTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ntp-kod",
		Type:    config.CheckTypeNTP,
		Timeout: 5 * time.Second,
		NTP:     &config.NTPCheckConfig{Server: host, Port: port},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for stratum 0")
	}
}

func TestNTPCheckerShortResponse(t *testing.T) {
	host, port, cleanup := startNTPServer(t, func([]byte) []byte {
		return make([]byte, 10) // fewer than 48 bytes
	})
	defer cleanup()

	checker := NewNTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ntp-short",
		Type:    config.CheckTypeNTP,
		Timeout: 5 * time.Second,
		NTP:     &config.NTPCheckConfig{Server: host, Port: port},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for short response")
	}
}

func TestNTPCheckerNoResponse(t *testing.T) {
	// Point at a closed port with a short timeout: the read must fail.
	checker := NewNTPChecker()
	cfg := &config.CheckConfig{
		Name:    "test-ntp-timeout",
		Type:    config.CheckTypeNTP,
		Timeout: 500 * time.Millisecond,
		NTP:     &config.NTPCheckConfig{Server: "127.0.0.1", Port: 1},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure when no server responds")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestNTPCheckerMissingConfig(t *testing.T) {
	checker := NewNTPChecker()
	cfg := &config.CheckConfig{Name: "test-ntp-nil", Type: config.CheckTypeNTP}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil NTP config")
	}
	if result.Error != "missing NTP check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
