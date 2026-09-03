package check

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"github.com/miekg/dns"
)

// newFakeDNS starts an in-process DNS server for tests, invoking handler for
// each query. Returns the listen address (host:port) and a cleanup func.
func newFakeDNS(t *testing.T, handler dns.HandlerFunc) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: handler}
	go func() { _ = srv.ActivateAndServe() }()
	return pc.LocalAddr().String(), func() { _ = srv.Shutdown() }
}

// newRecordDNS starts a fake resolver that answers A, AAAA and TXT queries for
// any name, and NXDOMAIN for any name under .invalid. Keeps the record tests
// off the public internet.
func newRecordDNS(t *testing.T) (string, func()) {
	t.Helper()
	return newFakeDNS(t, func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		if len(r.Question) == 0 {
			_ = w.WriteMsg(m)
			return
		}
		q := r.Question[0]
		if strings.Contains(q.Name, ".invalid.") {
			m.SetRcode(r, dns.RcodeNameError)
			_ = w.WriteMsg(m)
			return
		}
		hdr := dns.RR_Header{Name: q.Name, Rrtype: q.Qtype, Class: dns.ClassINET, Ttl: 300}
		switch q.Qtype {
		case dns.TypeA:
			m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: net.IPv4(8, 8, 8, 8)})
		case dns.TypeAAAA:
			m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.ParseIP("2001:4860:4860::8888")})
		case dns.TypeTXT:
			m.Answer = append(m.Answer, &dns.TXT{Hdr: hdr, Txt: []string{"v=spf1 -all"}})
		}
		_ = w.WriteMsg(m)
	})
}

func TestDNSCheckerSOA(t *testing.T) {
	addr, closeFn := newFakeDNS(t, func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		if len(r.Question) > 0 && r.Question[0].Qtype == dns.TypeSOA {
			m.Answer = append(m.Answer, &dns.SOA{
				Hdr:     dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
				Ns:      "ns1.example.com.",
				Mbox:    "hostmaster.example.com.",
				Serial:  2024010101,
				Refresh: 7200,
				Retry:   3600,
				Expire:  1209600,
				Minttl:  3600,
			})
		}
		_ = w.WriteMsg(m)
	})
	defer closeFn()

	want := "ns1.example.com. hostmaster.example.com. 2024010101 7200 3600 1209600 3600"
	cfg := &config.CheckConfig{
		Name:    "test-soa",
		Type:    config.CheckTypeDNS,
		Timeout: 5 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "example.com",
			Server:     addr,
			RecordType: "SOA",
			Expected:   []string{want},
		},
	}

	result := NewDNSChecker().Run(context.Background(), cfg, nil)
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(result.DNSAnswers) != 1 || result.DNSAnswers[0] != want {
		t.Errorf("expected answer %q, got %v", want, result.DNSAnswers)
	}
}

func TestDNSCheckerSOANotFound(t *testing.T) {
	addr, closeFn := newFakeDNS(t, func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r) // success rcode, but no SOA answer
		_ = w.WriteMsg(m)
	})
	defer closeFn()

	cfg := &config.CheckConfig{
		Name:    "test-soa-missing",
		Type:    config.CheckTypeDNS,
		Timeout: 5 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "example.com",
			Server:     addr,
			RecordType: "SOA",
		},
	}

	result := NewDNSChecker().Run(context.Background(), cfg, nil)
	if result.Success {
		t.Fatal("expected failure when no SOA record is returned")
	}
	if !strings.Contains(result.Error, "no SOA record") {
		t.Errorf("expected 'no SOA record' error, got: %s", result.Error)
	}
}

func TestDNSCheckerARecord(t *testing.T) {
	addr, closeFn := newRecordDNS(t)
	defer closeFn()

	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-a-record",
		Type:    config.CheckTypeDNS,
		Timeout: 2 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "example.com",
			Server:     addr,
			RecordType: "A",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if len(result.DNSAnswers) == 0 {
		t.Error("expected non-empty DNSAnswers")
	}
	if result.DNSQueryTime == 0 {
		t.Error("expected non-zero DNSQueryTime")
	}
}

func TestDNSCheckerAAAARecord(t *testing.T) {
	addr, closeFn := newRecordDNS(t)
	defer closeFn()

	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-aaaa-record",
		Type:    config.CheckTypeDNS,
		Timeout: 2 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "example.com",
			Server:     addr,
			RecordType: "AAAA",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	for _, answer := range result.DNSAnswers {
		if !strings.Contains(answer, ":") {
			t.Errorf("expected IPv6 address containing ':', got %s", answer)
		}
	}
}

func TestDNSCheckerTXTRecord(t *testing.T) {
	addr, closeFn := newRecordDNS(t)
	defer closeFn()

	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-txt-record",
		Type:    config.CheckTypeDNS,
		Timeout: 2 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "example.com",
			Server:     addr,
			RecordType: "TXT",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if len(result.DNSAnswers) == 0 {
		t.Error("expected non-empty answers for TXT record")
	}
}

func TestDNSCheckerExpectedMatch(t *testing.T) {
	addr, closeFn := newRecordDNS(t)
	defer closeFn()

	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-expected-match",
		Type:    config.CheckTypeDNS,
		Timeout: 2 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "dns.example.com",
			Server:     addr,
			RecordType: "A",
			Expected:   []string{"8.8.8.8"},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestDNSCheckerExpectedMismatch(t *testing.T) {
	addr, closeFn := newRecordDNS(t)
	defer closeFn()

	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-expected-mismatch",
		Type:    config.CheckTypeDNS,
		Timeout: 2 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "dns.example.com",
			Server:     addr,
			RecordType: "A",
			Expected:   []string{"1.2.3.4"},
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for mismatched expected value")
	}
	if !strings.Contains(result.Error, "expected values not found") {
		t.Errorf("expected 'expected values not found' error, got: %s", result.Error)
	}
}

func TestDNSCheckerInvalidDomain(t *testing.T) {
	addr, closeFn := newRecordDNS(t)
	defer closeFn()

	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-invalid-domain",
		Type:    config.CheckTypeDNS,
		Timeout: 2 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "thisdomain.doesnotexist.invalid",
			Server:     addr,
			RecordType: "A",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for invalid domain")
	}
}

func TestDNSCheckerUnsupportedType(t *testing.T) {
	addr, closeFn := newRecordDNS(t)
	defer closeFn()

	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name:    "test-unsupported-type",
		Type:    config.CheckTypeDNS,
		Timeout: 2 * time.Second,
		DNS: &config.DNSCheckConfig{
			Domain:     "example.com",
			Server:     addr,
			RecordType: "INVALID",
		},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for unsupported record type")
	}
	if !strings.Contains(result.Error, "unsupported record type") {
		t.Errorf("expected 'unsupported record type' error, got: %s", result.Error)
	}
}

func TestDNSCheckerMissingConfig(t *testing.T) {
	checker := NewDNSChecker()
	cfg := &config.CheckConfig{
		Name: "test-nil-dns",
		Type: config.CheckTypeDNS,
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil DNS config")
	}
	if result.Error != "missing DNS check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
