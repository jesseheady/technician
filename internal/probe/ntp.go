package probe

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"net"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

// NTP epoch starts 1900-01-01; Unix epoch starts 1970-01-01.
// The difference is 70 years worth of seconds (including 17 leap years).
const ntpEpochOffset = 2208988800

// ntpPacket is a minimal NTPv4 packet (48 bytes).
type ntpPacket struct {
	LiVnMode       uint8 // LI (2 bits) | VN (3 bits) | Mode (3 bits)
	Stratum        uint8
	Poll           int8
	Precision      int8
	RootDelay      uint32
	RootDispersion uint32
	ReferenceID    uint32
	RefTimeSec     uint32
	RefTimeFrac    uint32
	OrigTimeSec    uint32
	OrigTimeFrac   uint32
	RxTimeSec      uint32
	RxTimeFrac     uint32
	TxTimeSec      uint32
	TxTimeFrac     uint32
}

func ntpTimeToTime(sec, frac uint32) time.Time {
	secs := int64(sec) - ntpEpochOffset
	nanos := (int64(frac) * 1e9) >> 32
	return time.Unix(secs, nanos)
}

type NTPProber struct{}

func NewNTPProber() *NTPProber {
	return &NTPProber{}
}

func (p *NTPProber) Type() config.ProbeType {
	return config.ProbeTypeNTP
}

func (p *NTPProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypeNTP, site)

	if cfg.NTP == nil {
		result.Error = "missing NTP probe configuration"
		return result
	}

	ncfg := cfg.NTP
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", ncfg.Server, ncfg.Port)

	// Resolve and dial UDP with context timeout.
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		result.Error = fmt.Sprintf("NTP dial failed: %v", err)
		return result
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	conn.SetDeadline(deadline)

	// Build NTPv4 client request (mode 3).
	req := &ntpPacket{
		LiVnMode: 0x23, // LI=0, VN=4, Mode=3 (client)
	}

	// Record originate timestamp (T1).
	t1 := time.Now()

	buf := make([]byte, 48)
	buf[0] = req.LiVnMode
	if _, err := conn.Write(buf); err != nil {
		result.Duration = time.Since(t1)
		result.Error = fmt.Sprintf("NTP write failed: %v", err)
		return result
	}

	// Read response.
	resp := make([]byte, 48)
	n, err := conn.Read(resp)
	t4 := time.Now() // T4: destination timestamp
	if err != nil {
		result.Duration = time.Since(t1)
		result.Error = fmt.Sprintf("NTP read failed: %v", err)
		return result
	}
	if n < 48 {
		result.Duration = time.Since(t1)
		result.Error = fmt.Sprintf("NTP response too short: %d bytes", n)
		return result
	}

	// Parse response.
	var pkt ntpPacket
	pkt.LiVnMode = resp[0]
	pkt.Stratum = resp[1]
	pkt.Poll = int8(resp[2])
	pkt.Precision = int8(resp[3])
	pkt.RootDelay = binary.BigEndian.Uint32(resp[4:8])
	pkt.RootDispersion = binary.BigEndian.Uint32(resp[8:12])
	pkt.ReferenceID = binary.BigEndian.Uint32(resp[12:16])
	pkt.RxTimeSec = binary.BigEndian.Uint32(resp[32:36])
	pkt.RxTimeFrac = binary.BigEndian.Uint32(resp[36:40])
	pkt.TxTimeSec = binary.BigEndian.Uint32(resp[40:44])
	pkt.TxTimeFrac = binary.BigEndian.Uint32(resp[44:48])

	// Validate response: stratum 0 = "kiss-o'-death", LI=3 = unsynchronized.
	li := pkt.LiVnMode >> 6
	if li == 3 {
		result.Duration = t4.Sub(t1)
		result.Error = "NTP server is unsynchronized (LI=3)"
		return result
	}
	if pkt.Stratum == 0 {
		result.Duration = t4.Sub(t1)
		result.Error = "NTP kiss-of-death received (stratum 0)"
		return result
	}

	// T2: receive timestamp at server, T3: transmit timestamp at server.
	t2 := ntpTimeToTime(pkt.RxTimeSec, pkt.RxTimeFrac)
	t3 := ntpTimeToTime(pkt.TxTimeSec, pkt.TxTimeFrac)

	// Clock offset: ((T2 - T1) + (T3 - T4)) / 2
	offset := (t2.Sub(t1) + t3.Sub(t4)) / 2
	// Round-trip delay: (T4 - T1) - (T3 - T2)
	rtt := t4.Sub(t1) - t3.Sub(t2)

	result.Duration = t4.Sub(t1)
	result.NTPOffsetMs = float64(offset.Nanoseconds()) / 1e6
	result.NTPStratum = int(pkt.Stratum)
	result.NTPRTT = rtt
	result.Success = true

	slog.Debug("NTP probe completed",
		"name", cfg.Name,
		"server", addr,
		"offset_ms", math.Round(result.NTPOffsetMs*100)/100,
		"stratum", result.NTPStratum,
		"rtt", rtt,
	)

	return result
}
