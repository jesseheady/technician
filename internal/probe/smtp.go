package probe

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

type SMTPProber struct{}

func NewSMTPProber() *SMTPProber {
	return &SMTPProber{}
}

func (p *SMTPProber) Type() config.ProbeType {
	return config.ProbeTypeSMTP
}

func (p *SMTPProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypeSMTP, site)

	if cfg.SMTP == nil {
		result.Error = "missing SMTP probe configuration"
		return result
	}

	scfg := cfg.SMTP
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", scfg.Host, scfg.Port)

	start := time.Now()

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("connecting to %s: %v", addr, err)
		return result
	}

	client, err := smtp.NewClient(conn, scfg.Host)
	if err != nil {
		conn.Close()
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("creating SMTP client: %v", err)
		return result
	}
	defer client.Close()

	hostname := "technician.local"
	if err := client.Hello(hostname); err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("EHLO failed: %v", err)
		return result
	}

	result.Duration = time.Since(start)
	result.Success = true

	slog.Debug("SMTP probe completed",
		"name", cfg.Name,
		"host", addr,
		"duration", result.Duration,
	)

	return result
}
