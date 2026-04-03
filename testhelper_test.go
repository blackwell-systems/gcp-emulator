package gcpemulator_test

import (
	"context"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	eventarc "github.com/blackwell-systems/gcp-eventarc-emulator"
	iam "github.com/blackwell-systems/gcp-iam-emulator"
	kms "github.com/blackwell-systems/gcp-kms-emulator"
	sm "github.com/blackwell-systems/gcp-secret-manager-emulator"

	"github.com/blackwell-systems/gcp-emulator/internal/gateway"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/test/bufconn"
)

// dupRegPanic is a sentinel type used to identify panics caused by duplicate
// gRPC service registration (reflection.Register called by each emulator).
type dupRegPanic struct{ msg string }

// tolerantLogger wraps the default gRPC logger but converts Fatal calls for
// duplicate service registration into panics that can be recovered. This is
// necessary because each emulator library calls reflection.Register internally,
// and gRPC fatals on duplicate service names.
type tolerantLogger struct {
	grpclog.LoggerV2
}

func (l *tolerantLogger) Fatal(args ...any) {
	msg := fmt.Sprint(args...)
	if strings.Contains(msg, "duplicate service registration") {
		panic(dupRegPanic{msg: msg})
	}
	l.LoggerV2.Fatal(args...)
}

func (l *tolerantLogger) Fatalln(args ...any) {
	msg := fmt.Sprintln(args...)
	if strings.Contains(msg, "duplicate service registration") {
		panic(dupRegPanic{msg: msg})
	}
	l.LoggerV2.Fatalln(args...)
}

func (l *tolerantLogger) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "duplicate service registration") {
		panic(dupRegPanic{msg: msg})
	}
	l.LoggerV2.Fatalf(format, args...)
}

func init() {
	grpclog.SetLoggerV2(&tolerantLogger{LoggerV2: grpclog.NewLoggerV2(os.Stderr, os.Stderr, os.Stderr)})
}

// safeRegister calls a Register function, recovering from duplicate reflection
// registration panics that are expected when composing multiple emulators.
func safeRegister(fn func() error) error {
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(dupRegPanic); !ok {
					panic(r) // re-panic if not a duplicate registration
				}
				// Swallow duplicate reflection registration — harmless.
			}
		}()
		err = fn()
	}()
	return err
}

// setupEmulator creates an in-process gRPC server with all four emulator
// services registered. It returns a bufconn listener for in-process gRPC
// testing, a real TCP address for the REST gateway, and a cleanup function.
func setupEmulator(t *testing.T) (lis *bufconn.Listener, grpcAddr string, cleanup func()) {
	t.Helper()

	lis = bufconn.Listen(1024 * 1024)
	grpcSrv := grpc.NewServer()

	os.Setenv("IAM_MODE", "off")

	// Each emulator's Register function calls reflection.Register internally.
	// After the first one succeeds, subsequent calls trigger a fatal on
	// duplicate service name. We use safeRegister to recover from those.
	if err := safeRegister(func() error { return iam.Register(grpcSrv) }); err != nil {
		t.Fatalf("iam.Register error: %v", err)
	}
	if err := safeRegister(func() error { return sm.Register(grpcSrv) }); err != nil {
		t.Fatalf("sm.Register error: %v", err)
	}
	if err := safeRegister(func() error { return kms.Register(grpcSrv) }); err != nil {
		t.Fatalf("kms.Register error: %v", err)
	}
	if err := safeRegister(func() error { return eventarc.Register(grpcSrv) }); err != nil {
		t.Fatalf("eventarc.Register error: %v", err)
	}

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
