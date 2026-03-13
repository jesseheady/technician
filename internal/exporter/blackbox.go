package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jesseheady/technician/internal/config"
	"github.com/jesseheady/technician/internal/probe"
)

// BlackboxHandler implements a /probe endpoint compatible with
// Prometheus blackbox_exporter's probe interface.
type BlackboxHandler struct {
	probers map[string]probe.Prober
}

func NewBlackboxHandler() *BlackboxHandler {
	return &BlackboxHandler{
		probers: map[string]probe.Prober{
			"http_2xx": probe.NewHTTPProber(),
			"smtp":     probe.NewSMTPProber(),
			"tcp":      probe.NewTCPProber(),
			"dns":      probe.NewDNSProber(),
			"icmp":     probe.NewICMPProber(),
			"grpc":     probe.NewGRPCProber(),
		},
	}
}

func (h *BlackboxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	module := r.URL.Query().Get("module")
	if target == "" {
		http.Error(w, "missing target parameter", http.StatusBadRequest)
		return
	}
	if module == "" {
		module = "http_2xx"
	}

	prober, ok := h.probers[module]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown module: %s", module), http.StatusBadRequest)
		return
	}

	probeCfg := buildProbeConfig(target, module)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	result := prober.Run(ctx, probeCfg, nil)
	duration := time.Since(start)

	registry := prometheus.NewRegistry()

	probeSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_success",
		Help: "1 if the target responded successfully, 0 otherwise",
	})
	probeDuration := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_duration_seconds",
		Help: "End-to-end probe execution time in seconds",
	})

	registry.MustRegister(probeSuccess, probeDuration)

	if result.Success {
		probeSuccess.Set(1)
	} else {
		probeSuccess.Set(0)
	}
	probeDuration.Set(duration.Seconds())

	if result.StatusCode > 0 {
		httpStatus := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "probe_http_status_code",
			Help: "Status code returned by the HTTP target",
		})
		registry.MustRegister(httpStatus)
		httpStatus.Set(float64(result.StatusCode))
	}

	if result.DNSDuration > 0 {
		dns := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "probe_dns_lookup_time_seconds",
			Help: "DNS lookup duration",
		})
		registry.MustRegister(dns)
		dns.Set(result.DNSDuration.Seconds())
	}

	if result.TLSDuration > 0 {
		tls := prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "probe_tls_duration_seconds",
			Help: "TLS handshake duration",
		})
		registry.MustRegister(tls)
		tls.Set(result.TLSDuration.Seconds())
	}

	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)

	slog.Debug("Blackbox probe completed",
		"target", target,
		"module", module,
		"success", result.Success,
		"duration", duration,
	)
}

func buildProbeConfig(target, module string) *config.ProbeConfig {
	timeout := 30 * time.Second
	switch module {
	case "smtp":
		return &config.ProbeConfig{
			Name:    target,
			Type:    config.ProbeTypeSMTP,
			Timeout: timeout,
			SMTP: &config.SMTPProbeConfig{
				Host: target,
				Port: 25,
			},
		}
	case "tcp":
		return &config.ProbeConfig{
			Name:    target,
			Type:    config.ProbeTypeTCP,
			Timeout: timeout,
			TCP: &config.TCPProbeConfig{
				Host: target,
				Port: 443,
			},
		}
	case "dns":
		return &config.ProbeConfig{
			Name:    target,
			Type:    config.ProbeTypeDNS,
			Timeout: timeout,
			DNS: &config.DNSProbeConfig{
				Domain:     target,
				Server:     "8.8.8.8:53",
				RecordType: "A",
			},
		}
	case "icmp":
		return &config.ProbeConfig{
			Name:    target,
			Type:    config.ProbeTypeICMP,
			Timeout: timeout,
			ICMP: &config.ICMPProbeConfig{
				Host:  target,
				Count: 3,
			},
		}
	case "grpc":
		return &config.ProbeConfig{
			Name:    target,
			Type:    config.ProbeTypeGRPC,
			Timeout: timeout,
			GRPC: &config.GRPCProbeConfig{
				Host: target,
			},
		}
	default:
		return &config.ProbeConfig{
			Name:    target,
			Type:    config.ProbeTypeHTTP,
			Timeout: timeout,
			HTTP: &config.HTTPProbeConfig{
				URL:            target,
				Method:         "GET",
				ExpectedStatus: 200,
			},
		}
	}
}

// DebugHandler returns probe config for a module as JSON (useful for debugging).
func DebugHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		module := r.URL.Query().Get("module")
		if module == "" {
			module = "http_2xx"
		}

		cfg := buildProbeConfig(target, module)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)
	}
}
