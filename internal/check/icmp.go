package check

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/jesseheady/technician/internal/config"
)

// ICMPChecker sends ICMP Echo Request packets and measures round-trip times.
//
// ICMP requires elevated privileges on most platforms:
//   - Linux: root or CAP_NET_RAW capability (or unprivileged ICMP via udp fallback)
//   - macOS: the default user can send ICMP without root
type ICMPChecker struct{}

func NewICMPChecker() *ICMPChecker {
	return &ICMPChecker{}
}

func (p *ICMPChecker) Type() config.CheckType {
	return config.CheckTypeICMP
}

func (p *ICMPChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypeICMP, origin)

	if cfg.ICMP == nil {
		result.Error = "missing ICMP check configuration"
		return result
	}

	icfg := cfg.ICMP
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	count := icfg.Count
	if count <= 0 {
		count = 3
	}

	// Determine IP version and resolve the host.
	ipNetwork := "ip"
	switch icfg.IPVersion {
	case "4":
		ipNetwork = "ip4"
	case "6":
		ipNetwork = "ip6"
	}

	dst, err := net.ResolveIPAddr(ipNetwork, icfg.Host)
	if err != nil {
		result.Error = fmt.Sprintf("resolving %s: %v", icfg.Host, err)
		return result
	}

	isIPv6 := dst.IP.To4() == nil

	// Select the correct ICMP protocol and message type.
	var (
		privilegedNetwork   string
		unprivilegedNetwork string
		icmpType            icmp.Type
		listenAddr          string
	)
	if isIPv6 {
		privilegedNetwork = "ip6:ipv6-icmp"
		unprivilegedNetwork = "udp6"
		icmpType = ipv6.ICMPTypeEchoRequest
		listenAddr = "::"
	} else {
		privilegedNetwork = "ip4:icmp"
		unprivilegedNetwork = "udp4"
		icmpType = ipv4.ICMPTypeEcho
		listenAddr = "0.0.0.0"
	}

	// Try privileged ICMP first; fall back to unprivileged UDP mode on permission error.
	conn, err := icmp.ListenPacket(privilegedNetwork, listenAddr)
	if err != nil {
		slog.Debug("privileged ICMP listen failed, falling back to UDP",
			"name", cfg.Name,
			"error", err,
		)
		conn, err = icmp.ListenPacket(unprivilegedNetwork, listenAddr)
		if err != nil {
			result.Error = fmt.Sprintf("listen ICMP: %v", err)
			return result
		}
	}
	defer conn.Close()

	// Use a random ID to avoid collisions with other processes.
	echoID := rand.Intn(1 << 16)

	start := time.Now()
	var rtts []time.Duration
	sent := 0

	for seq := 0; seq < count; seq++ {
		// Check context before each ping.
		if ctx.Err() != nil {
			break
		}

		rtt, err := p.sendPing(ctx, conn, dst, icmpType, echoID, seq, timeout, isIPv6)
		sent++
		if err != nil {
			slog.Debug("ICMP ping failed",
				"name", cfg.Name,
				"host", icfg.Host,
				"seq", seq,
				"error", err,
			)
			continue
		}
		rtts = append(rtts, rtt)
	}

	result.Duration = time.Since(start)
	result.ICMPPacketsSent = sent
	result.ICMPPacketsRecv = len(rtts)

	if sent > 0 {
		result.ICMPPacketLoss = float64(sent-len(rtts)) / float64(sent) * 100.0
	}

	if len(rtts) > 0 {
		var total time.Duration
		minRTT := rtts[0]
		maxRTT := rtts[0]
		for _, rtt := range rtts {
			total += rtt
			if rtt < minRTT {
				minRTT = rtt
			}
			if rtt > maxRTT {
				maxRTT = rtt
			}
		}
		result.ICMPMinRTT = minRTT
		result.ICMPMaxRTT = maxRTT
		result.ICMPAvgRTT = total / time.Duration(len(rtts))
		result.Success = true
	}

	slog.Debug("ICMP check completed",
		"name", cfg.Name,
		"host", icfg.Host,
		"sent", result.ICMPPacketsSent,
		"recv", result.ICMPPacketsRecv,
		"loss", result.ICMPPacketLoss,
		"min_rtt", result.ICMPMinRTT,
		"avg_rtt", result.ICMPAvgRTT,
		"max_rtt", result.ICMPMaxRTT,
		"duration", result.Duration,
	)

	return result
}

// sendPing sends a single ICMP Echo Request and waits for the corresponding reply.
// It returns the round-trip time or an error if the reply was not received.
func (p *ICMPChecker) sendPing(
	ctx context.Context,
	conn *icmp.PacketConn,
	dst *net.IPAddr,
	icmpType icmp.Type,
	id, seq int,
	timeout time.Duration,
	isIPv6 bool,
) (time.Duration, error) {
	msg := icmp.Message{
		Type: icmpType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  seq,
			Data: []byte("technician-check"),
		},
	}

	var proto int
	if isIPv6 {
		proto = 58 // ICMPv6
	} else {
		proto = 1 // ICMPv4
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return 0, fmt.Errorf("marshal ICMP message: %w", err)
	}

	// Compute the deadline from context or timeout, whichever is sooner.
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	if err := conn.SetReadDeadline(deadline); err != nil {
		return 0, fmt.Errorf("set read deadline: %w", err)
	}

	pingStart := time.Now()

	_, err = conn.WriteTo(msgBytes, dst)
	if err != nil {
		return 0, fmt.Errorf("send ICMP: %w", err)
	}

	// Read replies until we find the matching echo reply or time out.
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}

		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if os.IsTimeout(err) {
				return 0, fmt.Errorf("timeout waiting for reply")
			}
			return 0, fmt.Errorf("read ICMP reply: %w", err)
		}

		reply, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil {
			continue // skip malformed packets
		}

		// Check for the expected echo reply type.
		var isEchoReply bool
		if isIPv6 {
			isEchoReply = reply.Type == ipv6.ICMPTypeEchoReply
		} else {
			isEchoReply = reply.Type == ipv4.ICMPTypeEchoReply
		}
		if !isEchoReply {
			continue
		}

		// Match by ID and sequence number.
		echo, ok := reply.Body.(*icmp.Echo)
		if !ok {
			continue
		}

		// In unprivileged (UDP) mode on Linux, the kernel rewrites the ID field.
		// Match by sequence number and payload content as a fallback.
		if echo.ID == id && echo.Seq == seq {
			return time.Since(pingStart), nil
		}

		// Fallback: match by sequence and payload for unprivileged sockets.
		if echo.Seq == seq && payloadMatches(echo.Data, []byte("technician-check")) {
			return time.Since(pingStart), nil
		}
	}
}

// payloadMatches checks if the received payload starts with the expected data.
// Some systems may pad or modify the payload, so we use a prefix match.
func payloadMatches(received, expected []byte) bool {
	return bytes.HasPrefix(received, expected)
}
