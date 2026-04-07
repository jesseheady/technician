package metrics

import (
	"log/slog"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/probe"
)

// maxProbeCardinality is the maximum number of distinct probe names that will
// be tracked as Prometheus labels. Beyond this limit, new names are ignored
// and a warning is logged. This prevents accidental label-cardinality
// explosion in Prometheus (each unique name × 33 metrics × site labels).
const maxProbeCardinality = 500

var (
	cardinalityMu    sync.Mutex
	seenProbeNames   = make(map[string]struct{})
	cardinalityLimit bool // true once we've logged the warning
)

var (
	probeUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_probe_up",
		Help: "1 if the target responded successfully, 0 if the check failed",
	}, []string{"type", "name", "region", "city", "country"})

	probeDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_probe_duration_seconds",
		Help: "End-to-end probe execution time in seconds",
	}, []string{"type", "name", "region", "city", "country"})

	httpResponseStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_response_status",
		Help: "Status code returned by the HTTP target",
	}, []string{"name", "region", "city", "country"})

	httpDNS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_dns_seconds",
		Help: "HTTP DNS lookup duration in seconds",
	}, []string{"name", "region", "city", "country"})

	httpTLS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_tls_seconds",
		Help: "HTTP TLS handshake duration in seconds",
	}, []string{"name", "region", "city", "country"})

	httpConnect = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_connect_seconds",
		Help: "HTTP TCP connect duration in seconds",
	}, []string{"name", "region", "city", "country"})

	httpTTFB = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_ttfb_seconds",
		Help: "HTTP time to first byte in seconds",
	}, []string{"name", "region", "city", "country"})

	httpTransfer = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_transfer_seconds",
		Help: "HTTP response transfer duration in seconds",
	}, []string{"name", "region", "city", "country"})

	httpResponseBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_response_bytes",
		Help: "HTTP response body size in bytes",
	}, []string{"name", "region", "city", "country"})

	browserTTFB = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_ttfb_ms",
		Help: "Browser Time to First Byte in milliseconds",
	}, []string{"name", "network", "device", "region", "city", "country"})

	browserFCP = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_fcp_ms",
		Help: "Browser First Contentful Paint in milliseconds",
	}, []string{"name", "network", "device", "region", "city", "country"})

	browserLCP = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_lcp_ms",
		Help: "Browser Largest Contentful Paint in milliseconds",
	}, []string{"name", "network", "device", "region", "city", "country"})

	browserCLS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_cls",
		Help: "Browser Cumulative Layout Shift score",
	}, []string{"name", "network", "device", "region", "city", "country"})

	browserINP = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_inp_ms",
		Help: "Browser Interaction to Next Paint in milliseconds (Core Web Vital; good ≤200ms)",
	}, []string{"name", "network", "device", "region", "city", "country"})

	browserDOMComplete = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_dom_complete_ms",
		Help: "Browser DOM complete time in milliseconds",
	}, []string{"name", "network", "device", "region", "city", "country"})

	browserTotalTransfer = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_total_transfer_bytes",
		Help: "Browser total transfer size in bytes",
	}, []string{"name", "network", "device", "region", "city", "country"})

	browserResourceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_resource_count",
		Help: "Browser total resource count",
	}, []string{"name", "network", "device", "region", "city", "country"})

	harResourceDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_har_resource_duration_ms",
		Help: "HAR resource duration in milliseconds by resource type",
	}, []string{"name", "resource_type", "region", "city", "country"})

	harResourceBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_har_resource_bytes",
		Help: "HAR resource size in bytes by resource type",
	}, []string{"name", "resource_type", "region", "city", "country"})

	budgetViolation = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_budget_violation",
		Help: "Whether a performance budget is violated (1=violated, 0=ok)",
	}, []string{"name", "metric", "region", "city", "country"})

	// TCP metrics
	tcpConnDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_tcp_connect_seconds",
		Help: "TCP connection duration in seconds",
	}, []string{"name", "region", "city", "country"})

	tcpTLSDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_tcp_tls_seconds",
		Help: "TCP TLS handshake duration in seconds",
	}, []string{"name", "region", "city", "country"})

	// DNS metrics
	dnsQueryDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_dns_query_seconds",
		Help: "DNS query duration in seconds",
	}, []string{"name", "region", "city", "country"})

	// ICMP metrics
	icmpPacketLoss = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_icmp_packet_loss_percent",
		Help: "ICMP packet loss percentage",
	}, []string{"name", "region", "city", "country"})

	icmpAvgRTT = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_icmp_avg_rtt_seconds",
		Help: "ICMP average round-trip time in seconds",
	}, []string{"name", "region", "city", "country"})

	// NTP metrics
	ntpOffsetMs = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_ntp_offset_ms",
		Help: "NTP clock offset in milliseconds (positive = local ahead of server)",
	}, []string{"name", "region", "city", "country"})

	ntpStratum = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_ntp_stratum",
		Help: "NTP server stratum level (1 = primary reference)",
	}, []string{"name", "region", "city", "country"})

	ntpRTT = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_ntp_rtt_seconds",
		Help: "NTP round-trip time in seconds",
	}, []string{"name", "region", "city", "country"})

	// UDP metrics
	udpRTT = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_udp_rtt_seconds",
		Help: "UDP round-trip time in seconds (send to response)",
	}, []string{"name", "region", "city", "country"})

	udpResponseBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_udp_response_bytes",
		Help: "UDP response size in bytes",
	}, []string{"name", "region", "city", "country"})

	// TLS certificate metrics
	tlsCertExpiryDays = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_tls_cert_expiry_days",
		Help: "Days until TLS certificate expires",
	}, []string{"name", "region", "city", "country"})

	tlsCertValid = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_tls_cert_valid",
		Help: "Whether the TLS certificate chain is valid (1=valid, 0=invalid)",
	}, []string{"name", "region", "city", "country"})

	// BGP metrics
	bgpPrefixVisible = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_bgp_prefix_visible",
		Help: "Whether the BGP prefix is visible in the global routing table (1=visible, 0=not visible)",
	}, []string{"name", "region", "city", "country"})

	bgpOriginASN = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_bgp_origin_asn",
		Help: "Observed origin AS number for the prefix",
	}, []string{"name", "region", "city", "country"})

	bgpOriginMatch = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_bgp_origin_match",
		Help: "Whether the origin ASN matches the expected value (1=match, 0=mismatch)",
	}, []string{"name", "region", "city", "country"})

	// Domain expiration metrics
	domainExpiryDays = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_domain_expiry_days",
		Help: "Days until domain registration expires",
	}, []string{"name", "region", "city", "country"})

	domainRegistered = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_domain_registered",
		Help: "Whether the domain is currently registered (1=registered, 0=not found)",
	}, []string{"name", "region", "city", "country"})

	// Infrastructure error indicator — recorded even when InfraError=true
	// so that silently-failing probes become visible in dashboards and alerts.
	probeInfraError = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_probe_infra_error",
		Help: "1 if the probe's own infrastructure failed (not the target), 0 otherwise",
	}, []string{"type", "name", "region", "city", "country"})

	// Degraded indicator
	probeDegraded = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_probe_degraded",
		Help: "Whether the probe response time exceeds the degraded threshold (1=degraded, 0=ok)",
	}, []string{"type", "name", "region", "city", "country"})
)

func init() {
	prometheus.MustRegister(
		probeUp,
		probeDuration,
		httpResponseStatus,
		httpDNS,
		httpTLS,
		httpConnect,
		httpTTFB,
		httpTransfer,
		httpResponseBytes,
		browserTTFB,
		browserFCP,
		browserLCP,
		browserCLS,
		browserINP,
		browserDOMComplete,
		browserTotalTransfer,
		browserResourceCount,
		harResourceDuration,
		harResourceBytes,
		budgetViolation,
		tcpConnDuration,
		tcpTLSDuration,
		dnsQueryDuration,
		icmpPacketLoss,
		icmpAvgRTT,
		ntpOffsetMs,
		ntpStratum,
		ntpRTT,
		udpRTT,
		udpResponseBytes,
		tlsCertExpiryDays,
		tlsCertValid,
		bgpPrefixVisible,
		bgpOriginASN,
		bgpOriginMatch,
		domainExpiryDays,
		domainRegistered,
		probeInfraError,
		probeDegraded,
	)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func RecordResult(result *probe.Result) {
	labels := siteLabels(result)
	typeStr := string(result.Type)

	if result.InfraError {
		// Record that this probe's infrastructure failed so dashboards and
		// alerts can surface silently-broken probes. We still skip the
		// target-level metrics (probe_up, duration, etc.) because they
		// would be misleading — the target was never actually tested.
		probeInfraError.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(1)
		return
	}

	// Clear any previous infra-error state now that the probe ran normally.
	probeInfraError.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(0)

	// Guard against label-cardinality explosion. If more unique probe names
	// appear than maxProbeCardinality, skip recording to protect Prometheus.
	cardinalityMu.Lock()
	if _, ok := seenProbeNames[result.Name]; !ok {
		if len(seenProbeNames) >= maxProbeCardinality {
			if !cardinalityLimit {
				slog.Warn("Probe cardinality limit reached, new probe names will not be recorded as metrics",
					"limit", maxProbeCardinality)
				cardinalityLimit = true
			}
			cardinalityMu.Unlock()
			return
		}
		seenProbeNames[result.Name] = struct{}{}
	}
	cardinalityMu.Unlock()

	up := float64(0)
	if result.Success {
		up = 1
	}

	probeUp.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(up)
	probeDuration.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(result.Duration.Seconds())

	switch result.Type {
	case config.ProbeTypeHTTP:
		recordHTTPMetrics(result, labels)
	case config.ProbeTypePlaywright:
		recordBrowserMetrics(result, labels)
	case config.ProbeTypeTCP:
		recordTCPMetrics(result, labels)
	case config.ProbeTypeDNS:
		recordDNSMetrics(result, labels)
	case config.ProbeTypeICMP:
		recordICMPMetrics(result, labels)
	case config.ProbeTypeNTP:
		recordNTPMetrics(result, labels)
	case config.ProbeTypeTLS:
		recordTLSMetrics(result, labels)
	case config.ProbeTypeUDP:
		recordUDPMetrics(result, labels)
	case config.ProbeTypeBGP:
		recordBGPMetrics(result, labels)
	case config.ProbeTypeDomainExpiry:
		recordDomainExpiryMetrics(result, labels)
	}

	degraded := float64(0)
	if result.Degraded {
		degraded = 1
	}
	probeDegraded.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(degraded)
}

func recordHTTPMetrics(result *probe.Result, labels labelSet) {
	httpResponseStatus.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.StatusCode))
	httpDNS.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.DNSDuration.Seconds())
	httpTLS.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TLSDuration.Seconds())
	httpConnect.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.ConnDuration.Seconds())
	httpTTFB.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TTFBDuration.Seconds())
	httpTransfer.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TransferDuration.Seconds())
	httpResponseBytes.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.ResponseBytes))
}

func recordBrowserMetrics(result *probe.Result, labels labelSet) {
	network := result.Labels["network"]
	device := result.Labels["device"]

	if result.WebVitals != nil {
		v := result.WebVitals
		browserTTFB.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(v.TTFB)
		browserFCP.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(v.FCP)
		browserLCP.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(v.LCP)
		browserCLS.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(v.CLS)
		browserINP.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(v.INP)
		browserDOMComplete.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(v.DOMComplete)
	}

	if result.HARData != nil {
		browserTotalTransfer.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(float64(result.HARData.TotalTransferBytes))
		browserResourceCount.WithLabelValues(result.Name, network, device, labels.code, labels.city, labels.country).Set(float64(result.ResourceCount))
		recordHARMetrics(result, labels)
	}
}

func recordHARMetrics(result *probe.Result, labels labelSet) {
	if result.HARData == nil {
		return
	}

	// Aggregate by resource type
	typeDurations := make(map[string]float64)
	typeBytes := make(map[string]int64)

	for _, entry := range result.HARData.Entries {
		rt := entry.ResourceType
		if rt == "" {
			rt = "other"
		}
		typeDurations[rt] += entry.Duration
		typeBytes[rt] += entry.TransferSize
	}

	for rt, dur := range typeDurations {
		harResourceDuration.WithLabelValues(result.Name, rt, labels.code, labels.city, labels.country).Set(dur)
	}
	for rt, bytes := range typeBytes {
		harResourceBytes.WithLabelValues(result.Name, rt, labels.code, labels.city, labels.country).Set(float64(bytes))
	}
}

func recordTCPMetrics(result *probe.Result, labels labelSet) {
	tcpConnDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TCPConnDuration.Seconds())
	tcpTLSDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TCPTLSDuration.Seconds())
}

func recordDNSMetrics(result *probe.Result, labels labelSet) {
	dnsQueryDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.DNSQueryTime.Seconds())
}

func recordICMPMetrics(result *probe.Result, labels labelSet) {
	icmpPacketLoss.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.ICMPPacketLoss)
	icmpAvgRTT.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.ICMPAvgRTT.Seconds())
}

func recordNTPMetrics(result *probe.Result, labels labelSet) {
	ntpOffsetMs.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.NTPOffsetMs)
	ntpStratum.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.NTPStratum))
	ntpRTT.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.NTPRTT.Seconds())
}

func recordUDPMetrics(result *probe.Result, labels labelSet) {
	udpRTT.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.UDPRTT.Seconds())
	udpResponseBytes.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.UDPResponseBytes))
}

func recordTLSMetrics(result *probe.Result, labels labelSet) {
	tlsCertExpiryDays.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.CertDaysRemaining))
	valid := float64(0)
	if result.CertValid {
		valid = 1
	}
	tlsCertValid.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(valid)
}

func recordBGPMetrics(result *probe.Result, labels labelSet) {
	visible := float64(0)
	if result.BGPPrefixVisible {
		visible = 1
	}
	bgpPrefixVisible.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(visible)
	bgpOriginASN.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.BGPOriginASN))
	match := float64(0)
	if result.BGPOriginMatch {
		match = 1
	}
	bgpOriginMatch.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(match)
}

func recordDomainExpiryMetrics(result *probe.Result, labels labelSet) {
	domainExpiryDays.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.DomainExpiryDays))
	registered := float64(0)
	if result.DomainRegistered {
		registered = 1
	}
	domainRegistered.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(registered)
}

func RecordBudgetViolation(probeName, metricName string, violated bool, site *config.Site) {
	labels := siteLabelsFromSite(site)
	v := float64(0)
	if violated {
		v = 1
	}
	budgetViolation.WithLabelValues(probeName, metricName, labels.code, labels.city, labels.country).Set(v)
}

type labelSet struct {
	code    string
	city    string
	country string
}

func siteLabels(result *probe.Result) labelSet {
	return labelSet{
		code:    result.Labels["region"],
		city:    result.Labels["city"],
		country: result.Labels["country"],
	}
}

func siteLabelsFromSite(site *config.Site) labelSet {
	if site == nil {
		return labelSet{}
	}
	return labelSet{
		code:    site.Code,
		city:    site.City,
		country: site.Country,
	}
}
