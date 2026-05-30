package cmd

import (
	"log/slog"

	"github.com/jesseheady/technician/internal/check"
	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/playwright"
)

// newCheckers builds the default set of checkers. Playwright is registered
// on a best-effort basis; if Node.js or script extraction fails, a warning
// is logged and browser checks are skipped.
func newCheckers(cfg *config.Config) map[config.CheckType]check.Checker {
	checkers := map[config.CheckType]check.Checker{
		config.CheckTypeHTTP:         check.NewHTTPChecker(),
		config.CheckTypeSMTP:         check.NewSMTPChecker(),
		config.CheckTypeTraceroute:   check.NewTracerouteChecker(),
		config.CheckTypeTCP:          check.NewTCPChecker(),
		config.CheckTypeDNS:          check.NewDNSChecker(),
		config.CheckTypeICMP:         check.NewICMPChecker(),
		config.CheckTypeGRPC:         check.NewGRPCChecker(),
		config.CheckTypeNTP:          check.NewNTPChecker(),
		config.CheckTypeTLS:          check.NewTLSChecker(),
		config.CheckTypeUDP:          check.NewUDPChecker(),
		config.CheckTypeBGP:          check.NewBGPChecker(),
		config.CheckTypeDomainExpiry: check.NewDomainExpirationChecker(),
	}

	if runner, err := playwright.NewRunner(); err != nil {
		slog.Warn("Playwright unavailable, browser checks will be skipped", "error", err)
	} else {
		checkers[config.CheckTypePlaywright] = check.NewPlaywrightChecker(runner.RunnerPath(), cfg.Playwright.MaxBrowsers)
		slog.Info("Playwright browser concurrency", "max_browsers", cfg.Playwright.MaxBrowsers)
	}

	return checkers
}
