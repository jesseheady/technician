package check

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

type SMTPChecker struct{}

func NewSMTPChecker() *SMTPChecker {
	return &SMTPChecker{}
}

func (p *SMTPChecker) Type() config.CheckType {
	return config.CheckTypeSMTP
}

func (p *SMTPChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypeSMTP, origin)

	if cfg.SMTP == nil {
		result.Error = "missing SMTP check configuration"
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

	if scfg.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("server %s does not advertise STARTTLS", addr)
			return result
		}
		tlsConfig := &tls.Config{
			ServerName:         scfg.Host,
			InsecureSkipVerify: scfg.SkipTLS, // #nosec G402 -- opt-in via skip_tls for self-signed mail servers
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("STARTTLS negotiation with %s failed: %v", addr, err)
			return result
		}
	}

	if scfg.Username != "" {
		// Config validation guarantees start_tls is set, so AUTH runs over TLS.
		auth := smtp.PlainAuth("", scfg.Username, scfg.Password, scfg.Host)
		if err := client.Auth(auth); err != nil {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("SMTP authentication to %s failed: %v", addr, err)
			return result
		}
	}

	result.Duration = time.Since(start)
	result.Success = true

	slog.Debug("SMTP check completed",
		"name", cfg.Name,
		"host", addr,
		"start_tls", scfg.StartTLS,
		"authenticated", scfg.Username != "",
		"duration", result.Duration,
	)

	return result
}
