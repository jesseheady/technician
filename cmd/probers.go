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
func newProbers() map[config.ProbeType]probe.Prober {
	probers := map[config.ProbeType]probe.Prober{
		config.ProbeTypeHTTP:       probe.NewHTTPProber(),
		config.ProbeTypeSMTP:       probe.NewSMTPProber(),
		config.ProbeTypeTraceroute: probe.NewTracerouteProber(),
		config.ProbeTypeTCP:        probe.NewTCPProber(),
		config.ProbeTypeDNS:        probe.NewDNSProber(),
		config.ProbeTypeICMP:       probe.NewICMPProber(),
		config.ProbeTypeGRPC:       probe.NewGRPCProber(),
	}

	if runner, err := playwright.NewRunner(); err != nil {
		slog.Warn("Playwright unavailable, browser probes will be skipped", "error", err)
	} else {
		probers[config.ProbeTypePlaywright] = probe.NewPlaywrightProber(runner.RunnerPath())
	}

	return probers
}
