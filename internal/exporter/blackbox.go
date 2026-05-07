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

	"github.com/m0nkey/technician/internal/check"
	"github.com/m0nkey/technician/internal/config"
)

// BlackboxHandler implements a /probe endpoint compatible with
// Prometheus blackbox_exporter's check interface.
type BlackboxHandler struct {
	checkers map[string]check.Checker
}

func NewBlackboxHandler() *BlackboxHandler {
	return &BlackboxHandler{
		checkers: map[string]check.Checker{
			"http_2xx": check.NewHTTPChecker(),
			"smtp":     check.NewSMTPChecker(),
			"tcp":      check.NewTCPChecker(),
			"dns":      check.NewDNSChecker(),
			"icmp":     check.NewICMPChecker(),
			"grpc":     check.NewGRPCChecker(),
			"udp":      check.NewUDPChecker(),
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

	checker, ok := h.checkers[module]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown module: %s", module), http.StatusBadRequest)
		return
	}

	checkCfg := buildCheckConfig(target, module)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	result := checker.Run(ctx, checkCfg, nil)
	duration := time.Since(start)

	registry := prometheus.NewRegistry()

	checkSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_success",
		Help: "1 if the target responded successfully, 0 otherwise",
	})
	checkDuration := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_duration_seconds",
		Help: "Total check duration in seconds",
	})

	registry.MustRegister(checkSuccess, checkDuration)

	if result.Success {
		checkSuccess.Set(1)
	} else {
		checkSuccess.Set(0)
	}
	checkDuration.Set(duration.Seconds())

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

	slog.Debug("Blackbox check completed",
		"target", target,
		"module", module,
		"success", result.Success,
		"duration", duration,
	)
}

func buildCheckConfig(target, module string) *config.CheckConfig {
	timeout := 30 * time.Second
	switch module {
	case "smtp":
		return &config.CheckConfig{
			Name:    target,
			Type:    config.CheckTypeSMTP,
			Timeout: timeout,
			SMTP: &config.SMTPCheckConfig{
				Host: target,
				Port: 25,
			},
		}
	case "tcp":
		return &config.CheckConfig{
			Name:    target,
			Type:    config.CheckTypeTCP,
			Timeout: timeout,
			TCP: &config.TCPCheckConfig{
				Host: target,
				Port: 443,
			},
		}
	case "dns":
		return &config.CheckConfig{
			Name:    target,
			Type:    config.CheckTypeDNS,
			Timeout: timeout,
			DNS: &config.DNSCheckConfig{
				Domain:     target,
				Server:     "8.8.8.8:53",
				RecordType: "A",
			},
		}
	case "icmp":
		return &config.CheckConfig{
			Name:    target,
			Type:    config.CheckTypeICMP,
			Timeout: timeout,
			ICMP: &config.ICMPCheckConfig{
				Host:  target,
				Count: 3,
			},
		}
	case "grpc":
		return &config.CheckConfig{
			Name:    target,
			Type:    config.CheckTypeGRPC,
			Timeout: timeout,
			GRPC: &config.GRPCCheckConfig{
				Host: target,
			},
		}
	case "udp":
		return &config.CheckConfig{
			Name:    target,
			Type:    config.CheckTypeUDP,
			Timeout: timeout,
			UDP: &config.UDPCheckConfig{
				Host: target,
				Port: 53,
				Send: "\x00",
			},
		}
	default:
		return &config.CheckConfig{
			Name:    target,
			Type:    config.CheckTypeHTTP,
			Timeout: timeout,
			HTTP: &config.HTTPCheckConfig{
				URL:            target,
				Method:         "GET",
				ExpectedStatus: 200,
			},
		}
	}
}

// DebugHandler returns check config for a module as JSON (useful for debugging).
func DebugHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		module := r.URL.Query().Get("module")
		if module == "" {
			module = "http_2xx"
		}

		cfg := buildCheckConfig(target, module)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cfg); err != nil {
			slog.Warn("Failed to encode blackbox debug response", "error", err)
		}
	}
}
