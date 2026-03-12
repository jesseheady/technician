package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
)

type TLSProber struct{}

func NewTLSProber() *TLSProber {
	return &TLSProber{}
}

func (p *TLSProber) Type() config.ProbeType {
	return config.ProbeTypeTLS
}

func (p *TLSProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypeTLS, site)

	if cfg.TLS == nil {
		result.Error = "missing TLS probe configuration"
		return result
	}

	tcfg := cfg.TLS
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	host := tcfg.Host
	// Default to port 443 if no port specified
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "443")
	}

	// Extract hostname for TLS ServerName (strip port)
	hostname, _, _ := net.SplitHostPort(host)

	start := time.Now()

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("connecting to %s: %v", host, err)
		return result
	}
	defer conn.Close()

	// Use InsecureSkipVerify so the handshake completes even with self-signed
	// or otherwise invalid certificates. We inspect and validate the chain
	// manually afterward — a cert monitoring probe needs to see the cert
	// even when the chain is broken.
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         hostname,
		InsecureSkipVerify: true,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("TLS handshake with %s: %v", host, err)
		return result
	}

	state := tlsConn.ConnectionState()
	result.Duration = time.Since(start)

	if len(state.PeerCertificates) == 0 {
		result.Error = fmt.Sprintf("no certificates presented by %s", host)
		return result
	}

	leaf := state.PeerCertificates[0]
	result.CertSubject = leaf.Subject.CommonName
	result.CertIssuer = leaf.Issuer.CommonName
	result.CertSANs = leaf.DNSNames
	result.CertExpiry = leaf.NotAfter
	result.CertChainLength = len(state.PeerCertificates)

	now := time.Now()
	daysRemaining := int(leaf.NotAfter.Sub(now).Hours() / 24)
	result.CertDaysRemaining = daysRemaining
	result.CertWarnDaysVal = tcfg.WarnDays
	result.CertCritDaysVal = tcfg.CriticalDays

	// Validate the full chain
	result.CertValid = validateChain(state.PeerCertificates, hostname)

	// Determine success: chain must be valid and cert not expired
	var errors []string

	if !result.CertValid {
		errors = append(errors, "certificate chain validation failed")
	}

	if now.After(leaf.NotAfter) {
		errors = append(errors, fmt.Sprintf("certificate expired on %s", leaf.NotAfter.Format("2006-01-02")))
	} else if now.Before(leaf.NotBefore) {
		errors = append(errors, fmt.Sprintf("certificate not valid until %s", leaf.NotBefore.Format("2006-01-02")))
	}

	if tcfg.CheckExpiry && daysRemaining <= tcfg.CriticalDays {
		errors = append(errors, fmt.Sprintf("certificate expires in %d days (critical threshold: %d)", daysRemaining, tcfg.CriticalDays))
	} else if tcfg.CheckExpiry && daysRemaining <= tcfg.WarnDays {
		errors = append(errors, fmt.Sprintf("certificate expires in %d days (warn threshold: %d)", daysRemaining, tcfg.WarnDays))
	}

	if len(errors) > 0 {
		result.Error = strings.Join(errors, "; ")
		// Only mark as failed (not just warning) if chain is invalid or cert is expired/not-yet-valid
		if !result.CertValid || now.After(leaf.NotAfter) || now.Before(leaf.NotBefore) ||
			(tcfg.CheckExpiry && daysRemaining <= tcfg.CriticalDays) {
			result.Success = false
		} else {
			// Warn-level expiry: probe succeeds but error field carries the warning
			result.Success = true
		}
	} else {
		result.Success = true
	}

	slog.Debug("TLS probe completed",
		"name", cfg.Name,
		"host", host,
		"subject", result.CertSubject,
		"issuer", result.CertIssuer,
		"expiry", result.CertExpiry.Format("2006-01-02"),
		"days_remaining", result.CertDaysRemaining,
		"chain_length", result.CertChainLength,
		"valid", result.CertValid,
		"duration", result.Duration,
	)

	return result
}

// validateChain verifies the certificate chain: checks that intermediates
// chain up to a trusted root, that no certificate in the chain is expired,
// and that the leaf matches the hostname.
func validateChain(certs []*x509.Certificate, hostname string) bool {
	if len(certs) == 0 {
		return false
	}

	// Build intermediate pool from presented chain (skip leaf at [0])
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}

	_, err := certs[0].Verify(x509.VerifyOptions{
		DNSName:       hostname,
		Intermediates: intermediates,
	})
	return err == nil
}
