// gcp-emulator: unified local emulator for Google Cloud Platform services.
//
// Runs Secret Manager, Eventarc, and IAM enforcement in a single process on
// a shared gRPC port (:9090) with a unified REST gateway (:8090).
//
// Usage:
//
//	gcp-emulator [flags]
//
// Environment Variables:
//
//	GCP_EMULATOR_GRPC_PORT   gRPC port (default: 9090)
//	GCP_EMULATOR_HTTP_PORT   REST gateway port (default: 8090)
//	GCP_EMULATOR_POLICY_FILE Path to IAM policy YAML file
//	IAM_MODE                 IAM enforcement: off, permissive, strict (default: off)
//	IAM_TRACE                Log IAM decisions: true/false (default: false)
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	eventarc "github.com/blackwell-systems/gcp-eventarc-emulator"
	iam "github.com/blackwell-systems/gcp-iam-emulator"
	kms "github.com/blackwell-systems/gcp-kms-emulator"
	sm "github.com/blackwell-systems/gcp-secret-manager-emulator"

	"github.com/blackwell-systems/gcp-emulator/internal/gateway"
)

var version = "0.1.0"

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		grpcPort   int
		httpPort   int
		policyFile string
		iamMode    string
		trace      bool
		watch      bool
	)

	cmd := &cobra.Command{
		Use:   "gcp-emulator",
		Short: "Unified GCP local emulator (Secret Manager, Eventarc, IAM)",
		Long: `gcp-emulator runs Secret Manager, Eventarc, and IAM enforcement
in a single process. All services share a gRPC port; a unified
REST gateway transcodes HTTP/JSON to gRPC.`,
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(grpcPort, httpPort, policyFile, iamMode, trace, watch)
		},
	}

	cmd.Flags().IntVar(&grpcPort, "grpc-port", getEnvInt("GCP_EMULATOR_GRPC_PORT", 9090), "gRPC port for all services")
	cmd.Flags().IntVar(&httpPort, "http-port", getEnvInt("GCP_EMULATOR_HTTP_PORT", 8090), "REST gateway port")
	cmd.Flags().StringVar(&policyFile, "policy", getEnv("GCP_EMULATOR_POLICY_FILE", ""), "Path to IAM policy YAML file")
	cmd.Flags().StringVar(&iamMode, "iam-mode", getEnv("IAM_MODE", "off"), "IAM enforcement: off, permissive, strict")
	cmd.Flags().BoolVar(&trace, "trace", os.Getenv("IAM_TRACE") == "true", "Log IAM authorization decisions")
	cmd.Flags().BoolVar(&watch, "watch", false, "Hot-reload policy file on change")

	return cmd
}

func run(grpcPort, httpPort int, policyFile, iamMode string, trace, watch bool) error {
	grpcAddr := fmt.Sprintf("localhost:%d", grpcPort)

	log.Printf("gcp-emulator v%s", version)
	log.Printf("IAM mode: %s", iamMode)

	// Create the shared gRPC server.
	grpcSrv := grpc.NewServer()
	reflection.Register(grpcSrv)

	// 1. Register IAM (control plane — must start first).
	iamOpts := []iam.Option{
		iam.WithTrace(trace),
	}
	if policyFile != "" {
		iamOpts = append(iamOpts, iam.WithPolicyFile(policyFile))
		log.Printf("IAM policy: %s", policyFile)
	}
	if err := iam.Register(grpcSrv, iamOpts...); err != nil {
		return fmt.Errorf("iam: %w", err)
	}

	// 2. Point data-plane services at the shared gRPC port for IAM checks.
	os.Setenv("IAM_EMULATOR_HOST", grpcAddr)
	os.Setenv("IAM_MODE", iamMode)

	// 3. Register Secret Manager.
	if err := sm.Register(grpcSrv); err != nil {
		return fmt.Errorf("secret manager: %w", err)
	}

	// 4. Register KMS.
	if err := kms.Register(grpcSrv); err != nil {
		return fmt.Errorf("kms: %w", err)
	}

	// 5. Register Eventarc (Eventarc + Publisher + Operations).
	if err := eventarc.Register(grpcSrv); err != nil {
		return fmt.Errorf("eventarc: %w", err)
	}

	// Start gRPC listener.
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	go func() {
		log.Printf("gRPC listening on :%d (Secret Manager, Eventarc, IAM)", grpcPort)
		if err := grpcSrv.Serve(lis); err != nil {
			log.Printf("gRPC server stopped: %v", err)
		}
	}()

	// Start unified REST gateway.
	gw, err := gateway.New(grpcAddr)
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}
	httpSrv, err := gw.Start(fmt.Sprintf(":%d", httpPort))
	if err != nil {
		return fmt.Errorf("http listen: %w", err)
	}
	log.Printf("REST gateway listening on :%d", httpPort)
	log.Printf("Ready — gRPC :%d  REST :%d", grpcPort, httpPort)

	// Optional policy hot-reload.
	if watch && policyFile != "" {
		go watchPolicy(policyFile, grpcSrv, iamOpts)
	}

	// Wait for shutdown signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	grpcSrv.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*1e9)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	log.Println("Stopped.")
	return nil
}

// watchPolicy reloads IAM policies when the file changes.
func watchPolicy(path string, grpcSrv *grpc.Server, iamOpts []iam.Option) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("watch: failed to create watcher: %v", err)
		return
	}
	defer w.Close()
	if err := w.Add(path); err != nil {
		log.Printf("watch: %v", err)
		return
	}
	log.Printf("Watching policy file: %s", path)
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Printf("Policy changed, reloading...")
				if err := iam.Register(grpcSrv, iamOpts...); err != nil {
					log.Printf("watch: reload failed: %v", err)
				} else {
					log.Printf("Policy reloaded.")
				}
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("watch error: %v", err)
		}
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}
