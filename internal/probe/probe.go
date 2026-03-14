package probe

import (
	"context"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

// AssertionResult records the outcome of a single body assertion.
type AssertionResult struct {
	Type    string `json:"type"`
	Target  string `json:"target"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"` // failure reason
}

type Result struct {
	Name      string
	Type      config.ProbeType
	Group     string
	Target    string // canonical hostname/domain for status page grouping
	Success    bool
	InfraError bool // true when the probe infrastructure itself failed (not the target)
	Duration   time.Duration
	Error      string
	Timestamp  time.Time

	// HTTP-specific
	StatusCode    int
	DNSDuration   time.Duration
	TLSDuration   time.Duration
	ConnDuration  time.Duration
	TTFBDuration  time.Duration
	TransferDuration time.Duration
	ResponseBytes int64
	Assertions    []AssertionResult

	// Browser-specific (Playwright)
	WebVitals    *WebVitals
	HARData      *HARData
	VideoPath    string
	ResourceCount int

	// Traceroute-specific
	Hops []TracerouteHop

	// TCP-specific
	TCPConnDuration time.Duration
	TCPTLSDuration  time.Duration

	// DNS-specific
	DNSAnswers   []string // resolved values
	DNSQueryTime time.Duration

	// ICMP-specific
	ICMPPacketsSent int
	ICMPPacketsRecv int
	ICMPPacketLoss  float64 // 0.0–100.0
	ICMPMinRTT      time.Duration
	ICMPAvgRTT      time.Duration
	ICMPMaxRTT      time.Duration

	// gRPC-specific
	GRPCStatus string // serving status from health check

	// NTP-specific
	NTPOffsetMs  float64       // clock offset in milliseconds (positive = local ahead)
	NTPStratum   int           // stratum level (1 = primary reference, 2+ = secondary)
	NTPRTT       time.Duration // round-trip time to NTP server

	// UDP-specific
	UDPRTT           time.Duration // round-trip time (send to response)
	UDPResponseBytes int           // number of bytes received

	// BGP-specific
	BGPOriginASN     int  // observed origin AS number
	BGPPrefixVisible bool // prefix visible in global routing table
	BGPOriginMatch   bool // origin ASN matches expected

	// Domain expiration-specific
	DomainExpiryDate  time.Time // when the domain registration expires
	DomainExpiryDays  int       // days until expiry
	DomainRegistrar   string    // registrar name/ID
	DomainRegistered  bool      // whether domain is currently registered
	DomainWarnDaysVal int       // configured warn threshold (days)
	DomainCritDaysVal int       // configured critical threshold (days)

	// TLS certificate-specific
	CertSubject       string    // leaf certificate subject CN
	CertIssuer        string    // issuer CN
	CertSANs          []string  // subject alternative names
	CertExpiry        time.Time // leaf certificate NotAfter
	CertDaysRemaining int       // days until expiry
	CertValid         bool      // full chain is valid (trusted root, not expired, hostname match)
	CertChainLength   int       // number of certificates in the chain
	CertWarnDaysVal   int       // configured warn threshold (days)
	CertCritDaysVal   int       // configured critical threshold (days)

	// Degraded flag (set when duration exceeds degraded_after threshold)
	Degraded bool

	// Extra metadata
	Labels map[string]string
}

type WebVitals struct {
	TTFB        float64 `json:"ttfb"`
	FCP         float64 `json:"fcp"`   // First Contentful Paint (optional; Core Web Vitals are LCP, INP, CLS)
	LCP         float64 `json:"lcp"`   // Largest Contentful Paint – good ≤2.5s
	CLS         float64 `json:"cls"`   // Cumulative Layout Shift – good ≤0.1
	INP         float64 `json:"inp"`   // Interaction to Next Paint – good ≤200ms
	DOMComplete float64 `json:"dom_complete"`
}

type HARData struct {
	Entries []HAREntry `json:"entries"`
	TotalTransferBytes int64 `json:"total_transfer_bytes"`
}

type HAREntry struct {
	URL          string  `json:"url"`
	ResourceType string  `json:"resource_type"`
	Duration     float64 `json:"duration"`
	TransferSize int64   `json:"transfer_size"`
	ResponseSize int64   `json:"response_size"`
	Status       int     `json:"status"`
}

type TracerouteHop struct {
	Hop     int     `json:"hop"`
	Host    string  `json:"host"`
	IP      string  `json:"ip"`
	ASN     int     `json:"asn"`
	AvgMs   float64 `json:"avg_ms"`
	LossPercent float64 `json:"loss_percent"`
}

// CertWarnDays returns the configured warning threshold, defaulting to 30.
func (r *Result) CertWarnDays() int {
	if r.CertWarnDaysVal > 0 {
		return r.CertWarnDaysVal
	}
	return 30
}

// CertCriticalDays returns the configured critical threshold, defaulting to 7.
func (r *Result) CertCriticalDays() int {
	if r.CertCritDaysVal > 0 {
		return r.CertCritDaysVal
	}
	return 7
}

// DomainWarnDays returns the configured warning threshold, defaulting to 30.
func (r *Result) DomainWarnDays() int {
	if r.DomainWarnDaysVal > 0 {
		return r.DomainWarnDaysVal
	}
	return 30
}

// DomainCriticalDays returns the configured critical threshold, defaulting to 7.
func (r *Result) DomainCriticalDays() int {
	if r.DomainCritDaysVal > 0 {
		return r.DomainCritDaysVal
	}
	return 7
}

type Prober interface {
	Type() config.ProbeType
	Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result
}

func NewResult(name string, probeType config.ProbeType, site *config.Site) *Result {
	r := &Result{
		Name:      name,
		Type:      probeType,
		Timestamp: time.Now(),
		Labels:    make(map[string]string),
	}
	if site != nil {
		r.Labels = site.Labels()
	}
	return r
}
