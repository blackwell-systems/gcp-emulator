// Package gateway provides the unified HTTP gateway for gcp-emulator.
//
// It routes incoming REST requests to the appropriate service handler
// based on URL path structure:
//   - /v1/projects/{p}/locations/... → Eventarc (grpc-gateway v2)
//   - /v1/projects/{p}/secrets/...   → Secret Manager (hand-rolled)
//   - /healthz, /readyz              → health endpoints
package gateway

import (
	"fmt"
	"net/http"
	"strings"

	eventarc "github.com/blackwell-systems/gcp-eventarc-emulator"
	kms "github.com/blackwell-systems/gcp-kms-emulator"
	sm "github.com/blackwell-systems/gcp-secret-manager-emulator"
)

// Gateway is the unified HTTP gateway for all gcp-emulator services.
type Gateway struct {
	smHandler       http.Handler
	eventarcHandler http.Handler
	kmsHandler      http.Handler
}

// New creates a unified HTTP gateway that proxies REST requests to all
// services running on the shared gRPC server at grpcAddr.
func New(grpcAddr string) (*Gateway, error) {
	smH, err := sm.NewGatewayHandler(grpcAddr)
	if err != nil {
		return nil, fmt.Errorf("gateway: secret manager: %w", err)
	}

	eaH, err := eventarc.NewGatewayHandler(grpcAddr)
	if err != nil {
		return nil, fmt.Errorf("gateway: eventarc: %w", err)
	}

	kmsH, err := kms.NewGatewayHandler(grpcAddr)
	if err != nil {
		return nil, fmt.Errorf("gateway: kms: %w", err)
	}

	return &Gateway{
		smHandler:       smH,
		eventarcHandler: eaH,
		kmsHandler:      kmsH,
	}, nil
}

// ServeHTTP implements http.Handler. Routes by path:
//   - /healthz, /readyz            → health check
//   - paths with /keyRings or /cryptoKeys → KMS
//   - paths with /locations/       → Eventarc
//   - all others                   → Secret Manager
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch path {
	case "/healthz", "/readyz":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}

	// KMS resources: .../locations/{loc}/keyRings/...
	// Check before the generic /locations/ check since KMS also uses locations.
	if strings.Contains(path, "/keyRings") || strings.Contains(path, "/cryptoKeys") {
		g.kmsHandler.ServeHTTP(w, r)
		return
	}

	// Eventarc resources always have /locations/ in the path.
	// Secret Manager resources never do.
	if strings.Contains(path, "/locations/") {
		g.eventarcHandler.ServeHTTP(w, r)
		return
	}

	g.smHandler.ServeHTTP(w, r)
}

// Start starts the unified HTTP gateway on httpAddr (non-blocking).
func (g *Gateway) Start(httpAddr string) (*http.Server, error) {
	srv := &http.Server{
		Addr:    httpAddr,
		Handler: g,
	}
	ln, err := newListener(httpAddr)
	if err != nil {
		return nil, err
	}
	go srv.Serve(ln) //nolint:errcheck
	return srv, nil
}
