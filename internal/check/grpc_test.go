package check

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jesseheady/technician/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// startGRPCHealthServer starts an in-process gRPC server exposing the standard
// health service. serving controls the reported status for the given service
// name (use "" for the overall server status). Returns the host:port and a
// cleanup func.
func startGRPCHealthServer(t *testing.T, service string, status healthpb.HealthCheckResponse_ServingStatus) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	srv := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus(service, status)
	healthpb.RegisterHealthServer(srv, hs)
	go srv.Serve(lis)

	return lis.Addr().String(), func() { srv.Stop() }
}

func TestGRPCCheckerServing(t *testing.T) {
	addr, cleanup := startGRPCHealthServer(t, "", healthpb.HealthCheckResponse_SERVING)
	defer cleanup()

	checker := NewGRPCChecker()
	cfg := &config.CheckConfig{
		Name:    "test-grpc-serving",
		Type:    config.CheckTypeGRPC,
		Timeout: 5 * time.Second,
		GRPC:    &config.GRPCCheckConfig{Host: addr},
	}
	origin := &config.Origin{ID: "test", City: "Test", Country: "XX"}

	result := checker.Run(context.Background(), cfg, origin)

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.GRPCStatus != "SERVING" {
		t.Errorf("expected SERVING, got %s", result.GRPCStatus)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestGRPCCheckerNotServing(t *testing.T) {
	addr, cleanup := startGRPCHealthServer(t, "", healthpb.HealthCheckResponse_NOT_SERVING)
	defer cleanup()

	checker := NewGRPCChecker()
	cfg := &config.CheckConfig{
		Name:    "test-grpc-not-serving",
		Type:    config.CheckTypeGRPC,
		Timeout: 5 * time.Second,
		GRPC:    &config.GRPCCheckConfig{Host: addr},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure when NOT_SERVING")
	}
	if result.GRPCStatus != "NOT_SERVING" {
		t.Errorf("expected NOT_SERVING, got %s", result.GRPCStatus)
	}
}

func TestGRPCCheckerNamedService(t *testing.T) {
	addr, cleanup := startGRPCHealthServer(t, "orders.v1.Orders", healthpb.HealthCheckResponse_SERVING)
	defer cleanup()

	checker := NewGRPCChecker()
	cfg := &config.CheckConfig{
		Name:    "test-grpc-named",
		Type:    config.CheckTypeGRPC,
		Timeout: 5 * time.Second,
		GRPC:    &config.GRPCCheckConfig{Host: addr, Service: "orders.v1.Orders"},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if !result.Success {
		t.Fatalf("expected success for named service, got error: %s", result.Error)
	}
}

func TestGRPCCheckerUnknownService(t *testing.T) {
	addr, cleanup := startGRPCHealthServer(t, "", healthpb.HealthCheckResponse_SERVING)
	defer cleanup()

	checker := NewGRPCChecker()
	cfg := &config.CheckConfig{
		Name:    "test-grpc-unknown",
		Type:    config.CheckTypeGRPC,
		Timeout: 5 * time.Second,
		GRPC:    &config.GRPCCheckConfig{Host: addr, Service: "does.not.Exist"},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for unregistered service")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestGRPCCheckerUnreachable(t *testing.T) {
	// Port 1 has no listener; the health RPC must fail within the timeout.
	checker := NewGRPCChecker()
	cfg := &config.CheckConfig{
		Name:    "test-grpc-unreachable",
		Type:    config.CheckTypeGRPC,
		Timeout: 1 * time.Second,
		GRPC:    &config.GRPCCheckConfig{Host: "127.0.0.1:1"},
	}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for unreachable host")
	}
	if result.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestGRPCCheckerConnReuse(t *testing.T) {
	addr, cleanup := startGRPCHealthServer(t, "", healthpb.HealthCheckResponse_SERVING)
	defer cleanup()

	checker := NewGRPCChecker()
	cfg := &config.CheckConfig{
		Name:    "test-grpc-reuse",
		Type:    config.CheckTypeGRPC,
		Timeout: 5 * time.Second,
		GRPC:    &config.GRPCCheckConfig{Host: addr},
	}

	checker.Run(context.Background(), cfg, nil)
	checker.Run(context.Background(), cfg, nil)

	if len(checker.conns) != 1 {
		t.Errorf("expected connection to be cached and reused, got %d conns", len(checker.conns))
	}
}

func TestGRPCCheckerMissingConfig(t *testing.T) {
	checker := NewGRPCChecker()
	cfg := &config.CheckConfig{Name: "test-grpc-nil", Type: config.CheckTypeGRPC}

	result := checker.Run(context.Background(), cfg, nil)

	if result.Success {
		t.Error("expected failure for nil gRPC config")
	}
	if result.Error != "missing gRPC check configuration" {
		t.Errorf("unexpected error: %s", result.Error)
	}
}
