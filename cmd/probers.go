package cmd

import (
	"log/slog"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/playwright"
	"github.com/jesseheady/technician/internal/probe"
)

// newProbers builds the default set of probers. Playwright is registered
// on a best-effort basis; if Node.js or script extraction fails, a warning
// is logged and browser probes are skipped.
func newProbers(cfg *config.Config) map[config.ProbeType]probe.Prober {
	probers := map[config.ProbeType]probe.Prober{
		config.ProbeTypeHTTP:         probe.NewHTTPProber(),
		config.ProbeTypeSMTP:         probe.NewSMTPProber(),
		config.ProbeTypeTraceroute:   probe.NewTracerouteProber(),
		config.ProbeTypeTCP:          probe.NewTCPProber(),
		config.ProbeTypeDNS:          probe.NewDNSProber(),
		config.ProbeTypeICMP:         probe.NewICMPProber(),
		config.ProbeTypeGRPC:         probe.NewGRPCProber(),
		config.ProbeTypeNTP:          probe.NewNTPProber(),
		config.ProbeTypeTLS:          probe.NewTLSProber(),
		config.ProbeTypeUDP:          probe.NewUDPProber(),
		config.ProbeTypeBGP:          probe.NewBGPProber(),
		config.ProbeTypeDomainExpiry: probe.NewDomainExpirationProber(),
	}

	if runner, err := playwright.NewRunner(); err != nil {
		slog.Warn("Playwright unavailable, browser probes will be skipped", "error", err)
	} else {
		probers[config.ProbeTypePlaywright] = probe.NewPlaywrightProber(runner.RunnerPath(), cfg.Playwright.MaxBrowsers)
		slog.Info("Playwright browser concurrency", "max_browsers", cfg.Playwright.MaxBrowsers)
	}

	return probers
}
