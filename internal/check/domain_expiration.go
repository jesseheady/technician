package check

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jesseheady/technician/internal/config"
)

// rdapDomain is the relevant subset of an RDAP domain response.
type rdapDomain struct {
	LDHName  string       `json:"ldhName"`
	Status   []string     `json:"status"`
	Events   []rdapEvent  `json:"events"`
	Entities []rdapEntity `json:"entities"`
}

type rdapEvent struct {
	Action string    `json:"eventAction"`
	Date   time.Time `json:"eventDate"`
}

type rdapEntity struct {
	Roles     []string `json:"roles"`
	Handle    string   `json:"handle"`
	PublicIDs []struct {
		Type       string `json:"type"`
		Identifier string `json:"identifier"`
	} `json:"publicIds"`
}

type DomainExpirationChecker struct{}

func NewDomainExpirationChecker() *DomainExpirationChecker {
	return &DomainExpirationChecker{}
}

func (p *DomainExpirationChecker) Type() config.CheckType {
	return config.CheckTypeDomainExpiry
}

func (p *DomainExpirationChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypeDomainExpiry, origin)

	if cfg.DomainExpiry == nil {
		result.Error = "missing domain expiration check configuration"
		return result
	}

	dcfg := cfg.DomainExpiry
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	start := time.Now()

	// Query RDAP via bootstrap service.
	apiURL := fmt.Sprintf("https://rdap.org/domain/%s", dcfg.Domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		result.Duration = time.Since(start)
		result.InfraError = true
		result.Error = fmt.Sprintf("building request: %v", err)
		return result
	}
	req.Header.Set("Accept", "application/rdap+json")

	resp, err := client.Do(req)
	if err != nil {
		result.Duration = time.Since(start)
		result.InfraError = true
		result.Error = fmt.Sprintf("RDAP query failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	result.Duration = time.Since(start)

	if resp.StatusCode == http.StatusNotFound {
		result.Error = fmt.Sprintf("domain %s not found in RDAP", dcfg.Domain)
		return result
	}
	if resp.StatusCode != http.StatusOK {
		result.InfraError = true
		result.Error = fmt.Sprintf("RDAP returned HTTP %d", resp.StatusCode)
		return result
	}

	var rdap rdapDomain
	if err := json.NewDecoder(resp.Body).Decode(&rdap); err != nil {
		result.InfraError = true
		result.Error = fmt.Sprintf("parsing RDAP response: %v", err)
		return result
	}

	result.DomainRegistered = true
	result.DomainWarnDaysVal = dcfg.WarnDays
	result.DomainCritDaysVal = dcfg.CriticalDays

	// Extract registrar from entities.
	for _, entity := range rdap.Entities {
		for _, role := range entity.Roles {
			if role == "registrar" {
				if entity.Handle != "" {
					result.DomainRegistrar = entity.Handle
				}
				for _, pid := range entity.PublicIDs {
					if pid.Type == "IANA Registrar ID" {
						result.DomainRegistrar = pid.Identifier
					}
				}
			}
		}
	}

	// Find expiration event.
	var expiryDate time.Time
	for _, event := range rdap.Events {
		if event.Action == "expiration" {
			expiryDate = event.Date
		}
	}

	if expiryDate.IsZero() {
		// Some TLDs don't include expiration in RDAP.
		result.Error = fmt.Sprintf("no expiration date found for %s", dcfg.Domain)
		result.Success = true
		return result
	}

	result.DomainExpiryDate = expiryDate
	now := time.Now()
	daysRemaining := int(expiryDate.Sub(now).Hours() / 24)
	result.DomainExpiryDays = daysRemaining

	// Evaluate thresholds.
	var errors []string
	if now.After(expiryDate) {
		errors = append(errors, fmt.Sprintf("domain expired on %s", expiryDate.Format("2006-01-02")))
	} else if daysRemaining <= dcfg.CriticalDays {
		errors = append(errors, fmt.Sprintf("domain expires in %d days (critical threshold: %d)", daysRemaining, dcfg.CriticalDays))
	} else if daysRemaining <= dcfg.WarnDays {
		errors = append(errors, fmt.Sprintf("domain expires in %d days (warn threshold: %d)", daysRemaining, dcfg.WarnDays))
	}

	if len(errors) > 0 {
		result.Error = strings.Join(errors, "; ")
		if now.After(expiryDate) || daysRemaining <= dcfg.CriticalDays {
			result.Success = false
		} else {
			result.Success = true // warn-level: check succeeds with warning
		}
	} else {
		result.Success = true
	}

	slog.Debug("Domain expiry check completed",
		"name", cfg.Name,
		"domain", dcfg.Domain,
		"expiry", expiryDate.Format("2006-01-02"),
		"days_remaining", daysRemaining,
		"registrar", result.DomainRegistrar,
		"duration", result.Duration,
	)

	return result
}
