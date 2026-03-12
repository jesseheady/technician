package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/monkeyWzr/technician/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type GRPCProber struct{}

func NewGRPCProber() *GRPCProber {
	return &GRPCProber{}
}

func (p *GRPCProber) Type() config.ProbeType {
	return config.ProbeTypeGRPC
}

func (p *GRPCProber) Run(ctx context.Context, cfg *config.ProbeConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.ProbeTypeGRPC, site)

	if cfg.GRPC == nil {
		result.Error = "missing gRPC probe configuration"
		return result
	}

	gcfg := cfg.GRPC
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var opts []grpc.DialOption
	if !gcfg.TLS {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else if gcfg.SkipTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})))
	}

	start := time.Now()

	conn, err := grpc.NewClient(gcfg.Host, opts...)
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("creating gRPC client for %s: %v", gcfg.Host, err)
		return result
	}
	defer conn.Close()

	healthClient := healthpb.NewHealthClient(conn)
	resp, err := healthClient.Check(ctx, &healthpb.HealthCheckRequest{
		Service: gcfg.Service,
	})
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("gRPC health check on %s: %v", gcfg.Host, err)
		return result
	}

	result.Duration = time.Since(start)
	result.GRPCStatus = resp.Status.String()
	result.Success = resp.Status == healthpb.HealthCheckResponse_SERVING

	slog.Debug("gRPC probe completed",
		"name", cfg.Name,
		"host", gcfg.Host,
		"service", gcfg.Service,
		"status", result.GRPCStatus,
		"duration", result.Duration,
	)

	return result
}
