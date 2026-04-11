package check

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/m0nkey/technician/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type grpcConnKey struct {
	host    string
	tls     bool
	skipTLS bool
}

type GRPCChecker struct {
	mu    sync.Mutex
	conns map[grpcConnKey]*grpc.ClientConn
}

func NewGRPCChecker() *GRPCChecker {
	return &GRPCChecker{
		conns: make(map[grpcConnKey]*grpc.ClientConn),
	}
}

// getConn returns a cached gRPC client connection for the given config,
// reusing connections across check runs to avoid repeated TCP/TLS setup.
func (p *GRPCChecker) getConn(host string, useTLS, skipTLS bool) (*grpc.ClientConn, error) {
	key := grpcConnKey{host: host, tls: useTLS, skipTLS: skipTLS}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[key]; ok {
		return c, nil
	}
	var opts []grpc.DialOption
	if !useTLS {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else if skipTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})))
	}
	conn, err := grpc.NewClient(host, opts...)
	if err != nil {
		return nil, err
	}
	p.conns[key] = conn
	return conn, nil
}

func (p *GRPCChecker) Type() config.CheckType {
	return config.CheckTypeGRPC
}

func (p *GRPCChecker) Run(ctx context.Context, cfg *config.CheckConfig, origin *config.Origin) *Result {
	result := NewResult(cfg.Name, config.CheckTypeGRPC, origin)

	if cfg.GRPC == nil {
		result.Error = "missing gRPC check configuration"
		return result
	}

	gcfg := cfg.GRPC
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	conn, err := p.getConn(gcfg.Host, gcfg.TLS, gcfg.SkipTLS)
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("creating gRPC client for %s: %v", gcfg.Host, err)
		return result
	}

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

	slog.Debug("gRPC check completed",
		"name", cfg.Name,
		"host", gcfg.Host,
		"service", gcfg.Service,
		"status", result.GRPCStatus,
		"duration", result.Duration,
	)

	return result
}
