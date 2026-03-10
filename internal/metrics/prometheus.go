package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/monkeyWzr/technician/internal/config"
	"github.com/monkeyWzr/technician/internal/probe"
)

var (
	probeUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_probe_up",
		Help: "Whether the probe was successful (1=up, 0=down)",
	}, []string{"type", "name", "site_code", "site_city", "site_country"})

	probeDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_probe_duration_seconds",
		Help: "Total probe duration in seconds",
	}, []string{"type", "name", "site_code", "site_city", "site_country"})

	httpResponseStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_response_status",
		Help: "HTTP response status code",
	}, []string{"name", "site_code", "site_city", "site_country"})

	httpDNS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_dns_seconds",
		Help: "HTTP DNS lookup duration in seconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	httpTLS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_tls_seconds",
		Help: "HTTP TLS handshake duration in seconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	httpConnect = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_connect_seconds",
		Help: "HTTP TCP connect duration in seconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	httpTTFB = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_ttfb_seconds",
		Help: "HTTP time to first byte in seconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	httpTransfer = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_transfer_seconds",
		Help: "HTTP response transfer duration in seconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	httpResponseBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_http_response_bytes",
		Help: "HTTP response body size in bytes",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserTTFB = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_ttfb_ms",
		Help: "Browser Time to First Byte in milliseconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserFCP = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_fcp_ms",
		Help: "Browser First Contentful Paint in milliseconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserLCP = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_lcp_ms",
		Help: "Browser Largest Contentful Paint in milliseconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserCLS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_cls",
		Help: "Browser Cumulative Layout Shift score",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserINP = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_inp_ms",
		Help: "Browser Interaction to Next Paint in milliseconds (Core Web Vital; good ≤200ms)",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserDOMComplete = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_dom_complete_ms",
		Help: "Browser DOM complete time in milliseconds",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserTotalTransfer = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_total_transfer_bytes",
		Help: "Browser total transfer size in bytes",
	}, []string{"name", "site_code", "site_city", "site_country"})

	browserResourceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_browser_resource_count",
		Help: "Browser total resource count",
	}, []string{"name", "site_code", "site_city", "site_country"})

	harResourceDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_har_resource_duration_ms",
		Help: "HAR resource duration in milliseconds by resource type",
	}, []string{"name", "resource_type", "site_code", "site_city", "site_country"})

	harResourceBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_har_resource_bytes",
		Help: "HAR resource size in bytes by resource type",
	}, []string{"name", "resource_type", "site_code", "site_city", "site_country"})

	budgetViolation = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "technician_budget_violation",
		Help: "Whether a performance budget is violated (1=violated, 0=ok)",
	}, []string{"name", "metric", "site_code", "site_city", "site_country"})
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
	)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func RecordResult(result *probe.Result) {
	labels := siteLabels(result)
	typeStr := string(result.Type)

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
	}
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
	if result.WebVitals != nil {
		v := result.WebVitals
		browserTTFB.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(v.TTFB)
		browserFCP.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(v.FCP)
		browserLCP.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(v.LCP)
		browserCLS.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(v.CLS)
		browserINP.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(v.INP)
		browserDOMComplete.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(v.DOMComplete)
	}

	if result.HARData != nil {
		browserTotalTransfer.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.HARData.TotalTransferBytes))
		browserResourceCount.WithLabelValues(result.Name, labels.code, labels.city, labels.country).Set(float64(result.ResourceCount))
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
		code:    result.Labels["site_code"],
		city:    result.Labels["site_city"],
		country: result.Labels["site_country"],
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
