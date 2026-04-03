package gcpemulator_test

import (
	"context"
	"net"
	"net/http/httptest"
	"os"
	"testing"

	eventarc "github.com/blackwell-systems/gcp-eventarc-emulator"
	iam "github.com/blackwell-systems/gcp-iam-emulator"
	kms "github.com/blackwell-systems/gcp-kms-emulator"
	sm "github.com/blackwell-systems/gcp-secret-manager-emulator"

	"github.com/blackwell-systems/gcp-emulator/internal/gateway"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"
)

// setupEmulator creates an in-process gRPC server with all four emulator
// services registered. It returns a bufconn listener for in-process gRPC
// testing, a real TCP address for the REST gateway, and a cleanup function.
func setupEmulator(t *testing.T) (lis *bufconn.Listener, grpcAddr string, cleanup func()) {
	t.Helper()

	lis = bufconn.Listen(1024 * 1024)
	grpcSrv := grpc.NewServer()

	os.Setenv("IAM_MODE", "off")

	if err := iam.Register(grpcSrv); err != nil {
		t.Fatalf("iam.Register error: %v", err)
	}
	if err := sm.Register(grpcSrv); err != nil {
		t.Fatalf("sm.Register error: %v", err)
	}
	if err := kms.Register(grpcSrv); err != nil {
		t.Fatalf("kms.Register error: %v", err)
	}
	if err := eventarc.Register(grpcSrv); err != nil {
		t.Fatalf("eventarc.Register error: %v", err)
	}
	reflection.Register(grpcSrv)

	go grpcSrv.Serve(lis) //nolint:errcheck

	tcpLis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen error: %v", err)
	}
	grpcAddr = tcpLis.Addr().String()

	go grpcSrv.Serve(tcpLis) //nolint:errcheck

	cleanup = func() {
		grpcSrv.Stop()
		lis.Close()
		tcpLis.Close()
	}
	return lis, grpcAddr, cleanup
}

// dialEmulator creates a gRPC client connection over the bufconn listener.
func dialEmulator(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient error: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// startGateway starts the unified REST gateway against the real TCP gRPC
// address and returns its base URL.
func startGateway(t *testing.T, grpcAddr string) string {
	t.Helper()

	gw, err := gateway.New(grpcAddr)
	if err != nil {
		t.Fatalf("gateway.New error: %v", err)
	}
	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	return ts.URL
}
