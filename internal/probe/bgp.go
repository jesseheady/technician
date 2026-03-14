package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
)

// ripeNetworkInfo is the response from stat.ripe.net/data/network-info.
type ripeNetworkInfo struct {
	Status string `json:"status"`
	Data   struct {
		ASNs   []string `json:"asns"`
		Prefix string   `json:"prefix"`
	} `json:"data"`
}

type BGPProber struct{}

func NewBGPProber() *BGPProber {
	return &BGPProber{}
}

func (p *BGPProber) Type() config.ProbeType {
	return config.ProbeTypeBGP
}

func (p *BGPProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypeBGP, site)

	if cfg.BGP == nil {
		result.Error = "missing BGP probe configuration"
		return result
	}

	bcfg := cfg.BGP
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	start := time.Now()

	// Query RIPE Stat for network info (origin ASN for the prefix).
	apiURL := fmt.Sprintf(
		"https://stat.ripe.net/data/network-info/data.json?resource=%s&sourceapp=technician",
		bcfg.Prefix,
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

	// Parse the first (primary) origin ASN.
	originASN, err := strconv.Atoi(info.Data.ASNs[0])
	if err != nil {
		result.InfraError = true
		result.Error = fmt.Sprintf("invalid ASN in response: %s", info.Data.ASNs[0])
		return result
	}
	result.BGPOriginASN = originASN

	// Validate origin if an expected ASN is configured.
	if bcfg.ExpectedOrigin > 0 && originASN != bcfg.ExpectedOrigin {
		result.BGPOriginMatch = false
		result.Error = fmt.Sprintf(
			"origin AS mismatch: expected AS%d, got AS%d (possible hijack)",
			bcfg.ExpectedOrigin, originASN,
		)
		return result
	}

	result.BGPOriginMatch = true
	result.Success = true

	slog.Debug("BGP probe completed",
		"name", cfg.Name,
		"prefix", bcfg.Prefix,
		"origin_asn", originASN,
		"expected_asn", bcfg.ExpectedOrigin,
		"duration", result.Duration,
	)

	return result
}
