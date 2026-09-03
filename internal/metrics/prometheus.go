package metrics

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
)

var (
	cardinalityMu sync.Mutex
	// maxCheckCardinality is the maximum number of distinct check names that
	// will be tracked as Prometheus labels. Beyond this limit, new names are
	// ignored and a warning is logged. This prevents accidental
	// label-cardinality explosion in Prometheus (each unique name × 33 metrics
	// × origin labels). Override via metrics.prometheus.max_check_cardinality.
	maxCheckCardinality = config.DefaultMaxCheckCardinality
	seenCheckNames      = make(map[string]struct{})
	cardinalityLimit    bool // true once we've logged the warning
)

// SetMaxCheckCardinality overrides the cardinality guard's limit. Call before
// the scheduler starts recording results; a limit <= 0 is ignored so an unset
// config keeps the default.
func SetMaxCheckCardinality(limit int) {
	if limit <= 0 {
		return
	}
	cardinalityMu.Lock()
	defer cardinalityMu.Unlock()
	maxCheckCardinality = limit
	cardinalityLimit = false
}

var (
	checkUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_check_healthy",
		Help: "1 if the target responded successfully, 0 if the check failed",
	}, []string{"type", "name", "region", "city", "country"})

	checkDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_check_duration_seconds",
		Help: "End-to-end check execution time in seconds",
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

	// WebSocket metrics
	wsConnectDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_ws_connect_seconds",
		Help: "WebSocket connection/handshake duration in seconds",
	}, []string{"name", "region", "city", "country"})

	wsMessageDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_ws_message_seconds",
		Help: "WebSocket send-to-response round-trip duration in seconds",
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
		Help: "Observed origin AS number for the prefix (0 when the prefix announces no route of its own)",
	}, []string{"name", "region", "city", "country"})

	bgpOriginMatch = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_bgp_origin_match",
		Help: "Whether the prefix and every more-specific inside it are announced by the expected origin AS (1=match, 0=mismatch)",
	}, []string{"name", "region", "city", "country"})

	bgpRPKIValid = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_bgp_rpki_valid",
		Help: "RPKI origin validation verdict for the prefix (1=valid, 0=invalid, -1=unknown/no ROA)",
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
	// so that silently-failing checks become visible in dashboards and alerts.
	checkInfraError = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_check_infra_error",
		Help: "1 if the check's own infrastructure failed (not the target), 0 otherwise",
	}, []string{"type", "name", "region", "city", "country"})

	// Degraded indicator
	checkDegraded = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_check_degraded",
		Help: "Whether the check response time exceeds the degraded threshold (1=degraded, 0=ok)",
	}, []string{"type", "name", "region", "city", "country"})

	// Status store persistence — counts every Save() that returned an
	// error. A persistent non-zero rate means the in-memory ring is no
	// longer being persisted; on container restart the status page will
	// be empty until check cycles refill it. The most common cause is a
	// data volume initialized under a previous root-running image.
	statusStoreWriteErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "technician_status_store_write_errors_total",
		Help: "Total status store persistence write failures since process start",
	})

	// Data-freshness signal — Unix timestamp of the most recent recorded
	// check result. Advances on any non-infra result, since that is when the
	// timing metrics get a fresh sample; infra errors (the target was never
	// reached) do not advance it, so a stretch of connectivity failures
	// freezes it just like a process gap does. Alerting uses time() minus
	// this value to detect data gaps and suppress the post-resume latency
	// spikes that inflate rolling averages (cold caches, NTP drift). See
	// prometheus/rules.yml (technician_freshness).
	lastRunTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "technician_last_run_timestamp_seconds",
		Help: "Unix timestamp of the most recent recorded check result (excludes infra errors)",
	})
)

func init() {
	prometheus.MustRegister(
		checkUp,
		checkDuration,
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
		wsConnectDuration,
		wsMessageDuration,
		tlsCertExpiryDays,
		tlsCertValid,
		bgpPrefixVisible,
		bgpOriginASN,
		bgpOriginMatch,
		bgpRPKIValid,
		domainExpiryDays,
		domainRegistered,
		checkInfraError,
		checkDegraded,
		statusStoreWriteErrors,
		lastRunTimestamp,
	)
}

// RecordStatusStoreWriteError increments the status store write-error
// counter. Call from every Save() call site that observed an error.
func RecordStatusStoreWriteError() {
	statusStoreWriteErrors.Inc()
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func RecordResult(result *check.Result) {
	labels := siteLabels(result)
	typeStr := string(result.Type)

	if result.InfraError {
		// Record that this check's infrastructure failed so dashboards and
		// alerts can surface silently-broken checks. We still skip the
		// target-level metrics (check_up, duration, etc.) because they
		// would be misleading — the target was never actually tested.
		checkInfraError.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(1)
		return
	}

	// Clear any previous infra-error state now that the check ran normally.
	checkInfraError.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(0)

	// Guard against label-cardinality explosion. If more unique check names
	// appear than maxCheckCardinality, skip recording to protect Prometheus.
	cardinalityMu.Lock()
	if _, ok := seenCheckNames[result.Name]; !ok {
		if len(seenCheckNames) >= maxCheckCardinality {
			if !cardinalityLimit {
				slog.Warn("Check cardinality limit reached, new check names will not be recorded as metrics",
					"limit", maxCheckCardinality)
				cardinalityLimit = true
			}
			cardinalityMu.Unlock()
			return
		}
		seenCheckNames[result.Name] = struct{}{}
	}
	cardinalityMu.Unlock()

	up := float64(0)
	if result.Success {
		up = 1
	}

	checkUp.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(up)
	checkDuration.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(result.Duration.Seconds())

	// Mark data as fresh: a real result was recorded (target reached), so the
	// timing series just got a new sample. Freshness tracks the most recent
	// such moment across all checks; alerting keys the staleness grace period
	// off it (prometheus/rules.yml → technician_freshness).
	lastRunTimestamp.Set(float64(time.Now().Unix()))

	switch result.Type {
	case config.CheckTypeHTTP:
		recordHTTPMetrics(result, labels)
	case config.CheckTypePlaywright:
		recordBrowserMetrics(result, labels)
	case config.CheckTypeTCP:
		recordTCPMetrics(result, labels)
	case config.CheckTypeDNS:
		recordDNSMetrics(result, labels)
	case config.CheckTypeICMP:
		recordICMPMetrics(result, labels)
	case config.CheckTypeNTP:
		recordNTPMetrics(result, labels)
	case config.CheckTypeTLS:
		recordTLSMetrics(result, labels)
	case config.CheckTypeUDP:
		recordUDPMetrics(result, labels)
	case config.CheckTypeWebSocket:
		recordWebSocketMetrics(result, labels)
	case config.CheckTypeBGP:
		recordBGPMetrics(result, labels)
	case config.CheckTypeDomainExpiry:
		recordDomainExpiryMetrics(result, labels)
	}

	degraded := float64(0)
	if result.Degraded {
		degraded = 1
	}
	checkDegraded.WithLabelValues(typeStr, result.Name, labels.code, labels.city, labels.country).Set(degraded)
}

func recordHTTPMetrics(result *check.Result, labels labelSet) {
	httpResponseStatus.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.StatusCode))
	httpDNS.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.DNSDuration.Seconds())
	httpTLS.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TLSDuration.Seconds())
	httpConnect.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.ConnDuration.Seconds())
	httpTTFB.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TTFBDuration.Seconds())
	httpTransfer.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TransferDuration.Seconds())
	httpResponseBytes.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.ResponseBytes))
}

func recordBrowserMetrics(result *check.Result, labels labelSet) {
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

func recordHARMetrics(result *check.Result, labels labelSet) {
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

func recordTCPMetrics(result *check.Result, labels labelSet) {
	tcpConnDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TCPConnDuration.Seconds())
	tcpTLSDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.TCPTLSDuration.Seconds())
}

func recordDNSMetrics(result *check.Result, labels labelSet) {
	dnsQueryDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.DNSQueryTime.Seconds())
}

func recordICMPMetrics(result *check.Result, labels labelSet) {
	icmpPacketLoss.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.ICMPPacketLoss)
	icmpAvgRTT.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.ICMPAvgRTT.Seconds())
}

func recordNTPMetrics(result *check.Result, labels labelSet) {
	ntpOffsetMs.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.NTPOffsetMs)
	ntpStratum.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.NTPStratum))
	ntpRTT.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.NTPRTT.Seconds())
}

func recordUDPMetrics(result *check.Result, labels labelSet) {
	udpRTT.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.UDPRTT.Seconds())
	udpResponseBytes.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.UDPResponseBytes))
}

func recordWebSocketMetrics(result *check.Result, labels labelSet) {
	wsConnectDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.WSConnectDuration.Seconds())
	wsMessageDuration.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(result.WSMessageDuration.Seconds())
}

func recordTLSMetrics(result *check.Result, labels labelSet) {
	tlsCertExpiryDays.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.CertDaysRemaining))
	valid := float64(0)
	if result.CertValid {
		valid = 1
	}
	tlsCertValid.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(valid)
}

func recordBGPMetrics(result *check.Result, labels labelSet) {
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

	// -1 means unknown: the prefix has no ROA, or the RPKI query did not answer.
	rpki := float64(-1)
	switch {
	case result.BGPRPKIStatus == "valid":
		rpki = 1
	case strings.HasPrefix(result.BGPRPKIStatus, "invalid"):
		rpki = 0
	}
	bgpRPKIValid.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(rpki)
}

func recordDomainExpiryMetrics(result *check.Result, labels labelSet) {
	domainExpiryDays.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.DomainExpiryDays))
	registered := float64(0)
	if result.DomainRegistered {
		registered = 1
	}
	domainRegistered.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(registered)
}

func RecordBudgetViolation(checkName, metricName string, violated bool, origin *config.Origin) {
	labels := originLabels(origin)
	v := float64(0)
	if violated {
		v = 1
	}
	budgetViolation.WithLabelValues(checkName, metricName, labels.code, labels.city, labels.country).Set(v)
}

type labelSet struct {
	code    string
	city    string
	country string
}

func siteLabels(result *check.Result) labelSet {
	return labelSet{
		code:    result.Labels["region"],
		city:    result.Labels["city"],
		country: result.Labels["country"],
	}
}

func originLabels(origin *config.Origin) labelSet {
	if origin == nil {
		return labelSet{}
	}
	return labelSet{
		code:    origin.ID,
		city:    origin.City,
		country: origin.Country,
	}
}
