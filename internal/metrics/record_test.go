package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

// gaugeValue is defined in prometheus_test.go and reused here.

// TestRecordResultDispatchesPerType drives one successful result through every
// type-specific recorder and asserts a representative gauge landed. This covers
// the RecordResult switch and each record*Metrics branch in one pass.
func TestRecordResultDispatchesPerType(t *testing.T) {
	// Unique names keep each case clear of the cardinality guard and of others.
	type tc struct {
		name   string
		mutate func(*check.Result)
		want   float64
		read   func(name string) prometheus.Gauge
	}
	tcs := []tc{
		{"rec-tcp", func(r *check.Result) { r.Type = config.CheckTypeTCP; r.TCPConnDuration = 2 * time.Second },
			2, func(n string) prometheus.Gauge { return tcpConnDuration.WithLabelValues(n, "", "", "") }},
		{"rec-dns", func(r *check.Result) { r.Type = config.CheckTypeDNS; r.DNSQueryTime = 3 * time.Second },
			3, func(n string) prometheus.Gauge { return dnsQueryDuration.WithLabelValues(n, "", "", "") }},
		{"rec-icmp", func(r *check.Result) { r.Type = config.CheckTypeICMP; r.ICMPPacketLoss = 42 },
			42, func(n string) prometheus.Gauge { return icmpPacketLoss.WithLabelValues(n, "", "", "") }},
		{"rec-ntp", func(r *check.Result) { r.Type = config.CheckTypeNTP; r.NTPStratum = 2 },
			2, func(n string) prometheus.Gauge { return ntpStratum.WithLabelValues(n, "", "", "") }},
		{"rec-udp", func(r *check.Result) { r.Type = config.CheckTypeUDP; r.UDPResponseBytes = 128 },
			128, func(n string) prometheus.Gauge { return udpResponseBytes.WithLabelValues(n, "", "", "") }},
		{"rec-tls", func(r *check.Result) { r.Type = config.CheckTypeTLS; r.CertDaysRemaining = 30 },
			30, func(n string) prometheus.Gauge { return tlsCertExpiryDays.WithLabelValues(n, "", "", "") }},
		{"rec-bgp", func(r *check.Result) { r.Type = config.CheckTypeBGP; r.BGPOriginASN = 64512 },
			64512, func(n string) prometheus.Gauge { return bgpOriginASN.WithLabelValues(n, "", "", "") }},
		{"rec-domain", func(r *check.Result) { r.Type = config.CheckTypeDomainExpiry; r.DomainExpiryDays = 90 },
			90, func(n string) prometheus.Gauge { return domainExpiryDays.WithLabelValues(n, "", "", "") }},
		{"rec-http", func(r *check.Result) { r.Type = config.CheckTypeHTTP; r.ResponseBytes = 512 },
			512, func(n string) prometheus.Gauge { return httpResponseBytes.WithLabelValues(n, "", "", "") }},
	}

	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			r := &check.Result{Name: c.name, Success: true, Duration: time.Second}
			c.mutate(r)
			RecordResult(r)

			if got := gaugeValue(t, c.read(c.name)); got != c.want {
				t.Errorf("%s gauge = %v, want %v", c.name, got, c.want)
			}
			// check_up is set for every recorded type.
			if up := gaugeValue(t, checkUp.WithLabelValues(string(r.Type), c.name, "", "", "")); up != 1 {
				t.Errorf("check_up = %v, want 1", up)
			}
		})
	}
}

// TestRecordResultInfraErrorSkipsTargetMetrics verifies an infra error records
// the infra-error gauge and does not mark the check up.
func TestRecordResultInfraErrorSkipsTargetMetrics(t *testing.T) {
	r := &check.Result{Name: "rec-infra", Type: config.CheckTypeHTTP, InfraError: true}
	RecordResult(r)

	if e := gaugeValue(t, checkInfraError.WithLabelValues("http", "rec-infra", "", "", "")); e != 1 {
		t.Errorf("infra_error = %v, want 1", e)
	}
}
