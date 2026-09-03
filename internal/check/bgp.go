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

// ripeRPKIValidation is the response from stat.ripe.net/data/rpki-validation.
// status is "valid", "invalid_asn", "invalid_length" or "unknown".
type ripeRPKIValidation struct {
	Data struct {
		Status string `json:"status"`
	} `json:"data"`
}

// ripeRoutingStatus is the response from stat.ripe.net/data/routing-status.
// It reports the origins of the exact prefix and the more-specific prefixes
// announced inside it, in one request.
//
// RIPE Stat caps more_specifics at 50 entries. A hijack announces one or a few
// more-specifics, so the cap only truncates the list for heavily deaggregated
// space that the operator announces itself.
type ripeRoutingStatus struct {
	Data struct {
		Origins []struct {
			Origin int `json:"origin"`
		} `json:"origins"`
		MoreSpecifics []struct {
			Prefix string `json:"prefix"`
			Origin int    `json:"origin"`
		} `json:"more_specifics"`
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

	// Query RIPE Stat for routing status: the origins of the prefix and the
	// more-specific prefixes announced inside it.
	apiURL := fmt.Sprintf(
		"%s/data/routing-status/data.json?resource=%s&sourceapp=technician",
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

	var info ripeRoutingStatus
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		result.Duration = time.Since(start)
		result.InfraError = true
		result.Error = fmt.Sprintf("parsing RIPE Stat response: %v", err)
		return result
	}

	result.Duration = time.Since(start)

	origins := make([]int, 0, len(info.Data.Origins))
	for _, o := range info.Data.Origins {
		origins = append(origins, o.Origin)
	}

	// A more-specific prefix from an unexpected origin is the classic hijack
	// shape. The attacker announces a longer prefix, wins longest-prefix-match,
	// and the origin of the covering prefix never changes. A check of the
	// covering prefix alone therefore stays green through the hijack.
	var rogue []string
	for _, ms := range info.Data.MoreSpecifics {
		if ms.Origin != bcfg.ExpectedOrigin {
			rogue = append(rogue, fmt.Sprintf("%s (AS%d)", ms.Prefix, ms.Origin))
		}
	}

	// An operator that deaggregates announces no route for the covering prefix
	// itself, only more-specifics. That prefix is still visible.
	result.BGPPrefixVisible = len(origins) > 0 || len(info.Data.MoreSpecifics) > 0

	// A rogue more-specific is reported before visibility, because it is the
	// more urgent finding when both are true.
	if len(rogue) > 0 {
		result.BGPOriginMatch = false
		result.Error = fmt.Sprintf(
			"more-specific prefix of %s announced by an unexpected origin: %s (possible hijack)",
			bcfg.Prefix, strings.Join(rogue, ", "),
		)
		return result
	}

	if !result.BGPPrefixVisible {
		result.Error = fmt.Sprintf("prefix %s not visible in global routing table", bcfg.Prefix)
		return result
	}

	// The gauge holds one number, so it reports the first origin. It stays 0
	// for a deaggregated prefix, which announces no route of its own.
	// Validation below uses the full set.
	if len(origins) > 0 {
		result.BGPOriginASN = origins[0]
	}

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

	// The origin is the expected one. Ask RPKI whether that origin holds a ROA
	// that authorizes this announcement. An "invalid" verdict is the strongest
	// hijack signal available, because it does not depend on the operator
	// keeping expected_origin current.
	result.BGPRPKIStatus = p.rpkiStatus(ctx, client, bcfg.Prefix, bcfg.ExpectedOrigin)
	if strings.HasPrefix(result.BGPRPKIStatus, "invalid") {
		result.Error = fmt.Sprintf(
			"RPKI %s for %s announced by AS%d: no ROA authorizes this announcement",
			result.BGPRPKIStatus, bcfg.Prefix, bcfg.ExpectedOrigin,
		)
		return result
	}

	result.Success = true

	slog.Debug("BGP check completed",
		"name", cfg.Name,
		"prefix", bcfg.Prefix,
		"origin_asns", origins,
		"more_specifics", len(info.Data.MoreSpecifics),
		"expected_asn", bcfg.ExpectedOrigin,
		"rpki_status", result.BGPRPKIStatus,
		"duration", result.Duration,
	)

	return result
}

// rpkiStatus returns the RIPE Stat RPKI verdict for the prefix and origin.
// It returns "unknown" when the prefix has no ROA, and also when the query
// fails: the origin comparison already succeeded, so a failed second query
// must not turn a good result into a failure. "unknown" is visible in
// technician_bgp_rpki_valid as -1.
//
// RPKI ROV validates the origin AS and prefix length against a ROA. It does
// not validate the rest of the AS path. An attacker who can inject a route
// (a compromised or complicit AS) can forge the path so it ends in the real
// origin AS, which this check will call "valid" if the resource holder's ROA
// permits the announced length — exactly what happened to the
// Softaculous/Virtualizor hijack in August 2026, via a loose ROA on Hetzner
// Online's announcing AS. See
// https://www.kentik.com/blog/latest-bgp-hijack-targets-hosting-software-vendor/
// A "valid" verdict here is not proof the announcement is legitimate; it is
// proof the resource holder's ROA does not forbid it.
func (p *BGPChecker) rpkiStatus(ctx context.Context, client *http.Client, prefix string, asn int) string {
	apiURL := fmt.Sprintf(
		"%s/data/rpki-validation/data.json?resource=AS%d&prefix=%s&sourceapp=technician",
		p.baseURL, asn, url.QueryEscape(prefix),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		slog.Debug("RPKI validation request failed", "prefix", prefix, "error", err)
		return "unknown"
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("RPKI validation query failed", "prefix", prefix, "error", err)
		return "unknown"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("RPKI validation returned non-OK", "prefix", prefix, "status", resp.StatusCode)
		return "unknown"
	}

	var v ripeRPKIValidation
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		slog.Debug("RPKI validation parse failed", "prefix", prefix, "error", err)
		return "unknown"
	}
	if v.Data.Status == "" {
		return "unknown"
	}
	return v.Data.Status
}
