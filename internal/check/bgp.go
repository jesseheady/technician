package check

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

const ripeStatBaseURL = "https://stat.ripe.net"

// ripeNetworkInfo is the response from stat.ripe.net/data/network-info.
type ripeNetworkInfo struct {
	Status string `json:"status"`
	Data   struct {
		ASNs   []string `json:"asns"`
		Prefix string   `json:"prefix"`
	} `json:"data"`
}

type BGPChecker struct {
	baseURL string // ponytail: seam for tests, not a config knob
}

func NewBGPChecker() *BGPChecker {
	return &BGPChecker{baseURL: ripeStatBaseURL}
}

func (p *BGPChecker) Type() config.CheckType {
	return config.CheckTypeBGP
}

func (p *BGPChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypeBGP, origin)

	if cfg.BGP == nil {
		result.Error = "missing BGP check configuration"
		return result
	}

	bcfg := cfg.BGP
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	start := time.Now()

	// Query RIPE Stat for network info (origin ASNs for the prefix).
	apiURL := fmt.Sprintf(
		"%s/data/network-info/data.json?resource=%s&sourceapp=technician",
		p.baseURL, url.QueryEscape(bcfg.Prefix),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		result.Duration = time.Since(start)
		result.InfraError = true
		result.Error = fmt.Sprintf("building request: %v", err)
		return result
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Duration = time.Since(start)
		result.InfraError = true
		result.Error = fmt.Sprintf("RIPE Stat query failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Duration = time.Since(start)
		result.InfraError = true
		result.Error = fmt.Sprintf("RIPE Stat returned HTTP %d", resp.StatusCode)
		return result
	}

	var info ripeNetworkInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		result.Duration = time.Since(start)
		result.InfraError = true
		result.Error = fmt.Sprintf("parsing RIPE Stat response: %v", err)
		return result
	}

	result.Duration = time.Since(start)

	// Check if prefix is visible in the global routing table.
	if len(info.Data.ASNs) == 0 {
		result.BGPPrefixVisible = false
		result.Error = fmt.Sprintf("prefix %s not visible in global routing table", bcfg.Prefix)
		return result
	}

	result.BGPPrefixVisible = true

	origins := make([]int, 0, len(info.Data.ASNs))
	for _, raw := range info.Data.ASNs {
		asn, err := strconv.Atoi(raw)
		if err != nil {
			result.InfraError = true
			result.Error = fmt.Sprintf("invalid ASN in response: %s", raw)
			return result
		}
		origins = append(origins, asn)
	}

	// The gauge holds one number, so it reports the first origin. Validation
	// below uses the full set.
	result.BGPOriginASN = origins[0]

	// Every announced origin must match the expected one. A hijacked prefix is
	// usually announced by the attacker and by its real operator at the same
	// time, so a check of the first origin alone can report a match while the
	// prefix is hijacked.
	var unexpected []string
	for _, asn := range origins {
		if asn != bcfg.ExpectedOrigin {
			unexpected = append(unexpected, "AS"+strconv.Itoa(asn))
		}
	}
	if len(unexpected) > 0 {
		result.BGPOriginMatch = false
		result.Error = fmt.Sprintf(
			"origin AS mismatch for %s: expected AS%d, found %s (possible hijack)",
			bcfg.Prefix, bcfg.ExpectedOrigin, strings.Join(unexpected, ", "),
		)
		return result
	}

	result.BGPOriginMatch = true
	result.Success = true

	slog.Debug("BGP check completed",
		"name", cfg.Name,
		"prefix", bcfg.Prefix,
		"origin_asns", origins,
		"expected_asn", bcfg.ExpectedOrigin,
		"duration", result.Duration,
	)

	return result
}
