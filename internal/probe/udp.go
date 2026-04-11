package probe

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

type UDPProber struct{}

func NewUDPProber() *UDPProber {
	return &UDPProber{}
}

func (p *UDPProber) Type() config.ProbeType {
	return config.ProbeTypeUDP
}

func (p *UDPProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypeUDP, site)

	if cfg.UDP == nil {
		result.Error = "missing UDP probe configuration"
		return result
	}

	ucfg := cfg.UDP
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// Determine payload.
	var payload []byte
	switch {
	case ucfg.SendHex != "":
		var err error
		payload, err = hex.DecodeString(ucfg.SendHex)
		if err != nil {
			result.Error = fmt.Sprintf("invalid send_hex: %v", err)
			return result
		}
	case ucfg.Send != "":
		payload = []byte(ucfg.Send)
	default:
		result.Error = "either send or send_hex must be specified"
		return result
	}

	addr := fmt.Sprintf("%s:%d", ucfg.Host, ucfg.Port)

	network := "udp"
	switch ucfg.IPVersion {
	case "4":
		network = "udp4"
	case "6":
		network = "udp6"
	}

	start := time.Now()

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("UDP dial to %s: %v", addr, err)
		return result
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	conn.SetDeadline(deadline)

	// Send the payload.
	sendStart := time.Now()
	if _, err := conn.Write(payload); err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("sending to %s: %v", addr, err)
		return result
	}

	// Check whether we expect a response.
	expectResponse := true
	if ucfg.ExpectResponse != nil {
		expectResponse = *ucfg.ExpectResponse
	}

	if !expectResponse && ucfg.ExpectRecv == "" {
		// Fire-and-forget: success if send completed without error.
		result.Duration = time.Since(start)
		result.Success = true
		slog.Debug("UDP probe completed (no response expected)",
			"name", cfg.Name,
			"host", addr,
			"duration", result.Duration,
		)
		return result
	}

	// Read response.
	maxBytes := ucfg.MaxResponseBytes
	if maxBytes == 0 {
		maxBytes = 4096
	}
	buf := make([]byte, maxBytes)
	n, err := conn.Read(buf)
	rtt := time.Since(sendStart)

	if err != nil {
		result.Duration = time.Since(start)
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			if expectResponse {
				result.Error = fmt.Sprintf("timeout waiting for response from %s", addr)
			} else {
				// Timeout with no ICMP error — port accepted the datagram silently.
				result.Success = true
			}
		} else {
			result.Error = fmt.Sprintf("reading from %s: %v", addr, err)
		}
		return result
	}

	result.UDPRTT = rtt
	result.UDPResponseBytes = n

	// Check expected response content.
	if ucfg.ExpectRecv != "" {
		received := string(buf[:n])
		if !strings.Contains(received, ucfg.ExpectRecv) {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("expected %q in response from %s, got %q",
				ucfg.ExpectRecv, addr, received)
			return result
		}
	}

	result.Duration = time.Since(start)
	result.Success = true

	slog.Debug("UDP probe completed",
		"name", cfg.Name,
		"host", addr,
		"duration", result.Duration,
		"rtt", result.UDPRTT,
		"response_bytes", result.UDPResponseBytes,
	)

	return result
}
