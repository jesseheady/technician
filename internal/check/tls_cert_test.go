package check

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

// newTestCert generates a self-signed certificate for testing.
// If expiry is zero, defaults to 1 year from now.
func newTestCert(t *testing.T, hostname string, notBefore, notAfter time.Time) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostname},
		DNSNames:              []string{hostname},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true, // self-signed
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
}

func startTLSServer(t *testing.T, cert tls.Certificate) net.Listener {
	t.Helper()
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatalf("start TLS listener: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Complete the TLS handshake on the server side before closing
			if tlsConn, ok := conn.(*tls.Conn); ok {
				tlsConn.Handshake()
			}
			conn.Close()
		}
	}()
	return ln
}

func TestTLSCheckerSuccess(t *testing.T) {
	cert := newTestCert(t, "localhost", time.Now().Add(-time.Hour), time.Now().Add(365*24*time.Hour))
	ln := startTLSServer(t, cert)
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	prober := NewTLSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tls-success",
		Type:    config.CheckTypeTLS,
		Timeout: 5 * time.Second,
		TLS: &config.TLSCheckConfig{
			Host:         net.JoinHostPort("localhost", itoa(addr.Port)),
			CheckExpiry:  true,
			WarnDays:     30,
			CriticalDays: 7,
		},
	}

	// Note: self-signed cert won't pass chain validation against system roots,
	// so we test the result fields are populated correctly and the chain
	// validation correctly reports invalid.
	result := prober.Run(context.Background(), cfg, nil)

	// Self-signed cert won't have a valid chain (no trusted root)
	if result.CertSubject != "localhost" {
		t.Errorf("expected subject 'localhost', got %q", result.CertSubject)
	}
	if result.CertChainLength != 1 {
		t.Errorf("expected chain length 1, got %d", result.CertChainLength)
	}
	if result.CertDaysRemaining < 360 {
		t.Errorf("expected ~365 days remaining, got %d", result.CertDaysRemaining)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
	if result.CertExpiry.IsZero() {
		t.Error("expected non-zero cert expiry")
	}
	// Self-signed cert: CertValid should be false (not trusted by system roots)
	if result.CertValid {
		t.Error("expected CertValid=false for self-signed cert")
	}
}

func TestTLSCheckerExpiredCert(t *testing.T) {
	cert := newTestCert(t, "localhost", time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	ln := startTLSServer(t, cert)
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	prober := NewTLSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tls-expired",
		Type:    config.CheckTypeTLS,
		Timeout: 5 * time.Second,
		TLS: &config.TLSCheckConfig{
			Host:         net.JoinHostPort("localhost", itoa(addr.Port)),
			CheckExpiry:  true,
			WarnDays:     30,
			CriticalDays: 7,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	// TLS handshake may fail on expired cert depending on Go version,
	// but if it succeeds we should see expiry data
	if result.Error == "" {
		t.Error("expected error for expired cert")
	}
	if result.Success {
		t.Error("expected failure for expired cert")
	}
}

func TestTLSCheckerCriticalExpiry(t *testing.T) {
	// Cert that expires in 3 days (within critical threshold of 7)
	cert := newTestCert(t, "localhost", time.Now().Add(-time.Hour), time.Now().Add(3*24*time.Hour))
	ln := startTLSServer(t, cert)
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	prober := NewTLSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tls-critical",
		Type:    config.CheckTypeTLS,
		Timeout: 5 * time.Second,
		TLS: &config.TLSCheckConfig{
			Host:         net.JoinHostPort("localhost", itoa(addr.Port)),
			CheckExpiry:  true,
			WarnDays:     30,
			CriticalDays: 7,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.CertDaysRemaining > 7 {
		t.Errorf("expected <=7 days remaining, got %d", result.CertDaysRemaining)
	}
	// Should fail: both chain invalid (self-signed) and critical expiry
	if result.Success {
		t.Error("expected failure for critical expiry + self-signed")
	}
}

func TestTLSCheckerConnectionRefused(t *testing.T) {
	prober := NewTLSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tls-refused",
		Type:    config.CheckTypeTLS,
		Timeout: 2 * time.Second,
		TLS: &config.TLSCheckConfig{
			Host:         "127.0.0.1:1",
			CheckExpiry:  true,
			WarnDays:     30,
			CriticalDays: 7,
		},
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for connection refused")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestTLSCheckerMissingConfig(t *testing.T) {
	prober := NewTLSChecker()
	cfg := &config.CheckConfig{
		Name: "test-tls-nil",
		Type: config.CheckTypeTLS,
	}

	result := prober.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil TLS config")
	}
	if result.Error != "missing TLS check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}

func TestTLSCheckerDefaultPort(t *testing.T) {
	// Just verify no panic when port is omitted — connection will fail
	// but the checkr should handle it gracefully
	prober := NewTLSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-tls-default-port",
		Type:    config.CheckTypeTLS,
		Timeout: 2 * time.Second,
		TLS: &config.TLSCheckConfig{
			Host:         "192.0.2.1", // TEST-NET, won't connect
			CheckExpiry:  true,
			WarnDays:     30,
			CriticalDays: 7,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := prober.Run(ctx, cfg, nil)

	if result.Success {
		t.Error("expected failure for unreachable host")
	}
}

func TestTLSCheckerType(t *testing.T) {
	prober := NewTLSChecker()
	if prober.Type() != config.CheckTypeTLS {
		t.Errorf("expected type %q, got %q", config.CheckTypeTLS, prober.Type())
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
