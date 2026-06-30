package check

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

// newFakeSMTP starts a minimal SMTP server for tests. It speaks the greeting,
// EHLO (optionally advertising STARTTLS), the STARTTLS upgrade, and accepts any
// AUTH. Returns the listen address and a cleanup func.
func newFakeSMTP(t *testing.T, advertiseSTARTTLS bool) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cert := selfSignedCert(t)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeSMTP(conn, advertiseSTARTTLS, cert)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func handleFakeSMTP(conn net.Conn, advertiseSTARTTLS bool, cert tls.Certificate) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	write := func(s string) { _, _ = conn.Write([]byte(s)) }

	write("220 fake ESMTP\r\n")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			if advertiseSTARTTLS {
				write("250-fake\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n")
			} else {
				write("250-fake\r\n250 AUTH PLAIN\r\n")
			}
		case cmd == "STARTTLS":
			write("220 Ready to start TLS\r\n")
			tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			if err := tconn.Handshake(); err != nil {
				return
			}
			conn = tconn
			br = bufio.NewReader(conn)
		case strings.HasPrefix(cmd, "AUTH"):
			write("235 2.7.0 Accepted\r\n")
		case cmd == "QUIT":
			write("221 Bye\r\n")
			return
		default:
			write("250 OK\r\n")
		}
	}
}

func smtpCfg(t *testing.T, addr string, c *config.SMTPCheckConfig) *config.CheckConfig {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)
	c.Host = host
	c.Port = port
	return &config.CheckConfig{Name: "smtp", Type: config.CheckTypeSMTP, Timeout: 5 * time.Second, SMTP: c}
}

func TestSMTPCheckerBasicConnectivity(t *testing.T) {
	addr, closeFn := newFakeSMTP(t, false)
	defer closeFn()
	cfg := smtpCfg(t, addr, &config.SMTPCheckConfig{})
	if r := NewSMTPChecker().Run(context.Background(), cfg, nil); !r.Success {
		t.Errorf("expected success, got error: %s", r.Error)
	}
}

func TestSMTPCheckerStartTLSNotAdvertised(t *testing.T) {
	addr, closeFn := newFakeSMTP(t, false) // no STARTTLS advertised
	defer closeFn()
	cfg := smtpCfg(t, addr, &config.SMTPCheckConfig{StartTLS: true})
	r := NewSMTPChecker().Run(context.Background(), cfg, nil)
	if r.Success {
		t.Fatal("expected failure when STARTTLS requested but not advertised")
	}
	if !strings.Contains(r.Error, "STARTTLS") {
		t.Errorf("expected STARTTLS error, got: %s", r.Error)
	}
}

func TestSMTPCheckerStartTLSAndAuth(t *testing.T) {
	addr, closeFn := newFakeSMTP(t, true)
	defer closeFn()
	cfg := smtpCfg(t, addr, &config.SMTPCheckConfig{
		StartTLS: true,
		SkipTLS:  true, // self-signed test cert
		Username: "user",
		Password: "pass",
	})
	if r := NewSMTPChecker().Run(context.Background(), cfg, nil); !r.Success {
		t.Errorf("expected success with STARTTLS+AUTH, got error: %s", r.Error)
	}
}
