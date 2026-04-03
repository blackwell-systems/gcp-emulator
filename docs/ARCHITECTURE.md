# Architecture: gcp-emulator

This document describes the internal architecture of
[`gcp-emulator`](https://github.com/blackwell-systems/gcp-emulator), the
unified single-process runtime that composes all Blackwell GCP service
emulators.

---

## 1. Overview

`gcp-emulator` is a Go binary that runs Secret Manager, KMS, Eventarc, and IAM
enforcement in a single process. All four gRPC services share one port (:9090);
a unified REST gateway on a second port (:8090) transcodes HTTP/JSON to gRPC
using grpc-gateway v2.

### Two-layer model

The codebase is split across two logical layers:

```
Layer 1 — Library layer (individual service repos)
  github.com/blackwell-systems/gcp-iam-emulator
  github.com/blackwell-systems/gcp-kms-emulator
  github.com/blackwell-systems/gcp-secret-manager-emulator
  github.com/blackwell-systems/gcp-eventarc-emulator

Layer 2 — Product layer (this repo)
  github.com/blackwell-systems/gcp-emulator
```

Each service emulator repo is a **Go library**. It exposes two public
functions at its module root (`Register` and `NewGatewayHandler`) and
publishes its own semver module. The service repos have no knowledge of each
other.

`gcp-emulator` is the **product**. It imports all four libraries, wires them
onto a shared `grpc.Server`, and builds the unified gateway and binary. It owns
the server lifecycle, ports, reflection, shutdown, and policy hot-reload.

### Why not docker-compose?

The previous orchestration approach (`gcp-iam-control-plane`) used
docker-compose to run each emulator as a separate container and route traffic
between them over the network. This approach had several problems:

- **Startup overhead**: Four containers must start, each listening for
  connections from the others before requests can succeed.
- **Network hops for IAM checks**: Every data-plane permission check required
  an inter-container TCP round-trip.
- **Fragile port mapping**: Consumers had to manage four separate ports and
  endpoints.
- **Heavyweight testing**: Integration tests required Docker to be running.

In-process composition eliminates all of this. Services share memory, IAM
checks are local gRPC calls on the same server (no network), consumers connect
to a single port, and tests run entirely in-process using `bufconn`.

---

## 2. Composition Pattern

### Register()

Every service emulator exposes a `Register` function at its module root:

```go
func Register(grpcSrv *grpc.Server, opts ...Option) error
```

`Register` constructs the service implementation and calls the generated
`RegisterXxxServer(grpcSrv, srv)` protobuf registration function. It does
**not** start a listener — the caller owns the `grpc.Server` lifecycle. It does
**not** call `reflection.Register` — that is the caller's responsibility and
must be done exactly once.

### NewGatewayHandler()

Every service emulator also exposes a gateway constructor:

```go
func NewGatewayHandler(grpcAddr string) (http.Handler, error)
```

`NewGatewayHandler` dials the shared gRPC server at `grpcAddr` (using insecure
credentials — local emulator, no TLS), registers the grpc-gateway v2 handler
for that service's proto file, and returns an `http.Handler`. The handler is
stateless with respect to routing: the unified gateway in `gcp-emulator`
decides which handler receives each request.

### reflection.Register — called once by the caller

`google.golang.org/grpc/reflection` registers a reflection service that allows
`grpcurl` and other tools to introspect the server's service descriptors. If
each individual `Register()` called it, the reflection service would be
registered multiple times on the same server, causing a duplicate registration
panic at startup.

`gcp-emulator`'s `main.go` calls `reflection.Register(grpcSrv)` once, after
creating the server and before registering any services.

### How main.go wires the four services

```go
// 1. Create the shared gRPC server and register reflection.
grpcSrv := grpc.NewServer()
reflection.Register(grpcSrv)

// 2. Register IAM first — data-plane services depend on it for permission
//    checks, so it must be on the server before IAM_EMULATOR_HOST is set.
if err := iam.Register(grpcSrv,
    iam.WithPolicyFile(policyFile),
    iam.WithTrace(trace),
); err != nil {
    return fmt.Errorf("iam: %w", err)
}

// 3. Point data-plane services at the shared gRPC addr for IAM checks.
os.Setenv("IAM_EMULATOR_HOST", grpcAddr)  // e.g. "localhost:9090"
os.Setenv("IAM_MODE", iamMode)            // "off" | "permissive" | "strict"

// 4. Register the three data-plane services.
sm.Register(grpcSrv)
kms.Register(grpcSrv)
eventarc.Register(grpcSrv)

// 5. Start the gRPC listener.
lis, _ := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
go grpcSrv.Serve(lis)

// 6. Start the unified REST gateway.
gw, _ := gateway.New(grpcAddr)
gw.Start(fmt.Sprintf(":%d", httpPort))
```

---

## 3. Architecture Diagram

```
                          gcp-emulator (single process)
 ┌────────────────────────────────────────────────────────────────────┐
 │                                                                    │
 │   grpc.Server  :9090                                               │
 │  ┌─────────────────────────────────────────────────────────────┐  │
 │  │  google.cloud.secretmanager.v1.SecretManagerService         │  │
 │  │  google.cloud.kms.v1.KeyManagementService                   │  │
 │  │  google.cloud.eventarc.v1.Eventarc                          │  │
 │  │  google.cloud.eventarc.publishing.v1.Publisher              │  │
 │  │  google.longrunning.Operations                              │  │
 │  │  google.iam.v1.IAMPolicy                                    │  │
 │  │  grpc.reflection.v1                                         │  │
 │  └─────────────────────────────────────────────────────────────┘  │
 │            ^                         ^                             │
 │            │ IAM_EMULATOR_HOST       │ local gRPC dial             │
 │            │ (permission checks)     │                             │
 │            └─────────────────────────┘                            │
 │                                                                    │
 │   gateway.Gateway  :8090                                           │
 │  ┌─────────────────────────────────────────────────────────────┐  │
 │  │  /keyRings, /cryptoKeys  ──────────────► kmsHandler         │  │
 │  │  /locations/             ──────────────► eventarcHandler    │  │
 │  │  :setIamPolicy etc.      ──────────────► iamHandler         │  │
 │  │  (default)               ──────────────► smHandler          │  │
 │  │  /healthz, /readyz        ─────────────► inline 200 JSON    │  │
 │  └────────────────┬────────────────────────────────────────────┘  │
 │                   │  each handler dials :9090 via grpc-gateway v2  │
 └───────────────────┼────────────────────────────────────────────────┘
                     │
          ┌──────────┴──────────┐
          │                     │
   GCP SDK / grpcurl      HTTP clients
   (gRPC, :9090)          (REST, :8090)
```

IAM enforcement: when `IAM_MODE` is `permissive` or `strict`, Secret Manager,
KMS, and Eventarc each read `IAM_EMULATOR_HOST` at startup (via
`gcp-emulator-auth`) and dial the same `:9090` gRPC server to call
`TestIamPermissions` before processing requests.

---

## 4. Unified REST Gateway Routing

`internal/gateway/gateway.go` implements `http.Handler`. It routes incoming
requests to one of four per-service grpc-gateway handlers based on URL path
content:

```go
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Path

    // Health endpoints.
    if path == "/healthz" || path == "/readyz" { ... return }

    // KMS: always has /keyRings or /cryptoKeys.
    // Must be checked before /locations/ because KMS paths also contain /locations/.
    if strings.Contains(path, "/keyRings") || strings.Contains(path, "/cryptoKeys") {
        g.kmsHandler.ServeHTTP(w, r); return
    }

    // Eventarc: always has /locations/ and never has /keyRings or /cryptoKeys.
    // Secret Manager paths never contain /locations/.
    if strings.Contains(path, "/locations/") {
        g.eventarcHandler.ServeHTTP(w, r); return
    }

    // IAM policy methods: verb suffix on any resource path.
    if strings.HasSuffix(path, ":setIamPolicy") ||
       strings.HasSuffix(path, ":getIamPolicy") ||
       strings.HasSuffix(path, ":testIamPermissions") {
        g.iamHandler.ServeHTTP(w, r); return
    }

    // Default: Secret Manager.
    g.smHandler.ServeHTTP(w, r)
}
```

### Routing table

| URL path pattern                                            | Handler         |
|-------------------------------------------------------------|-----------------|
| `/healthz`, `/readyz`                                       | Inline 200 JSON |
| contains `/keyRings` or `/cryptoKeys`                       | KMS             |
| contains `/locations/` (and not a KMS path)                 | Eventarc        |
| ends with `:setIamPolicy`, `:getIamPolicy`, `:testIamPermissions` | IAM        |
| all others                                                  | Secret Manager  |

This works without a service registry because each service uses a distinct
URL namespace:

- **Secret Manager**: `/v1/projects/{p}/secrets/...` — no `locations`, no
  `keyRings`
- **KMS**: `/v1/projects/{p}/locations/{l}/keyRings/...` — always has
  `keyRings` or `cryptoKeys`
- **Eventarc**: `/v1/projects/{p}/locations/{l}/triggers/...` (and channels,
  pipelines) — has `locations` but never `keyRings`
- **IAM policy methods**: any resource path followed by a colon-prefixed verb

The KMS check must precede the Eventarc `/locations/` check because KMS paths
also contain `/locations/`. The `strings.Contains` checks are O(n) on path
length — negligible cost for local development traffic.

---

## 5. IAM Enforcement

### Environment variables

| Variable            | Set by         | Read by                        |
|---------------------|----------------|-------------------------------|
| `IAM_EMULATOR_HOST` | `main.go`      | SM, KMS, Eventarc (via auth)  |
| `IAM_MODE`          | `main.go`      | SM, KMS, Eventarc (via auth)  |
| `IAM_TRACE`         | flag/env       | IAM emulator                  |

`main.go` calls `os.Setenv("IAM_EMULATOR_HOST", grpcAddr)` and
`os.Setenv("IAM_MODE", iamMode)` **after** IAM is registered but **before**
the data-plane services are registered. This ordering ensures that when SM,
KMS, and Eventarc initialize, the IAM address is already in the environment.

### Enforcement modes

| `IAM_MODE`    | Behavior                                                          |
|---------------|-------------------------------------------------------------------|
| `off`         | All requests allowed. No policy file required. Default.           |
| `permissive`  | IAM checks run; denials are logged but requests are not blocked.  |
| `strict`      | IAM checks run; unauthorized requests receive `PERMISSION_DENIED`.|

### How data-plane services check permissions

Each data-plane emulator (SM, KMS, Eventarc) imports
`github.com/blackwell-systems/gcp-emulator-auth`. On each request, the auth
library:

1. Reads the `x-emulator-principal` gRPC metadata header to identify the
   caller.
2. Reads `IAM_EMULATOR_HOST` to find the IAM gRPC endpoint.
3. Calls `TestIamPermissions` on the IAM service running on the same
   `grpc.Server`.
4. Based on `IAM_MODE`, either allows, logs, or blocks the request.

Because the IAM service is co-located on the same `grpc.Server`, permission
checks are in-process gRPC calls with no network latency.

### Policy file format

```yaml
roles:
  roles/secretmanager.admin:
    permissions:
      - secretmanager.secrets.create
      - secretmanager.secrets.get

projects:
  my-project:
    bindings:
      - role: roles/secretmanager.admin
        members:
          - serviceAccount:app@my-project.iam.gserviceaccount.com
```

The policy file is loaded at startup by `iam.Register` via
`iam.WithPolicyFile(path)`. With `--watch`, `main.go` uses `fsnotify` to
detect writes to the file and re-calls `iam.Register` on the running server,
replacing the in-memory policy without restarting.

---

## 6. Individual Emulator Repos

| Repo                           | Module path                                              | Version in gcp-emulator | gRPC services registered                                          |
|--------------------------------|----------------------------------------------------------|-------------------------|-------------------------------------------------------------------|
| `gcp-iam-emulator`             | `github.com/blackwell-systems/gcp-iam-emulator`          | v0.10.0                 | `google.iam.v1.IAMPolicy`                                         |
| `gcp-kms-emulator`             | `github.com/blackwell-systems/gcp-kms-emulator`          | v0.5.0                  | `google.cloud.kms.v1.KeyManagementService`                        |
| `gcp-secret-manager-emulator`  | `github.com/blackwell-systems/gcp-secret-manager-emulator` | v1.6.0                | `google.cloud.secretmanager.v1.SecretManagerService`              |
| `gcp-eventarc-emulator`        | `github.com/blackwell-systems/gcp-eventarc-emulator`     | v0.1.2                  | `google.cloud.eventarc.v1.Eventarc`, `google.cloud.eventarc.publishing.v1.Publisher`, `google.longrunning.Operations` |

Each repo also exposes standalone binaries (`cmd/server`, `cmd/server-rest`,
`cmd/server-dual`) for use when only one service is needed.

---

## 7. grpc-gateway v2

All REST gateways use `github.com/grpc-ecosystem/grpc-gateway/v2`.

### Why grpc-gateway v2

- Produces correct GCP-compatible HTTP status codes (404 for `NOT_FOUND`, 400
  for `INVALID_ARGUMENT`, etc.) rather than always returning 200.
- Marshals responses in the exact JSON format the GCP Go SDK expects, including
  proto field name casing and `Any` type encoding.
- HTTP/JSON bindings are proto-generated from the official googleapis proto
  files, so URL patterns exactly match the real GCP REST API.
- Hand-rolling an HTTP mux for each service would require manually maintaining
  URL-to-method mappings that already exist in the proto annotations.

### Regenerating gateway stubs

Each emulator repo contains a `buf.gen.yaml` that generates the grpc-gateway
handler registration file. Example from `gcp-eventarc-emulator`:

```yaml
version: v2
managed:
  enabled: true
  override:
    - file_option: go_package_prefix
      value: github.com/blackwell-systems/gcp-eventarc-emulator/internal/gen
plugins:
  - remote: buf.build/grpc-ecosystem/gateway:v2.28.0
    out: internal/gen
    opt:
      - paths=source_relative
      - generate_unbound_methods=true
inputs:
  - git_repo: https://github.com/googleapis/googleapis.git
    depth: 1
    paths:
      - google/cloud/eventarc/v1/eventarc.proto
      - google/cloud/eventarc/publishing/v1/publisher.proto
      - google/longrunning/operations.proto
```

Run `buf generate` in the emulator repo to regenerate. The `remote` plugin
pins the gateway generator version to match the `grpc-gateway/v2` dependency
in `go.mod`.

### types.go re-export pattern

The buf generator produces a `*.pb.gw.go` file that imports the service's
request and response types. Rather than also generating `*.pb.go` files (which
would conflict with the official `cloud.google.com/go/...` proto packages
already in the module graph), each emulator provides a hand-written `types.go`
in the generated package that re-exports the types as Go type aliases:

```go
// internal/gen/google/cloud/kms/v1/types.go
package kmsv1

import (
    kmspb "cloud.google.com/go/kms/apiv1/kmspb"
    "google.golang.org/grpc"
)

type KeyManagementServiceClient = kmspb.KeyManagementServiceClient
type KeyManagementServiceServer = kmspb.KeyManagementServiceServer

func NewKeyManagementServiceClient(cc grpc.ClientConnInterface) KeyManagementServiceClient {
    return kmspb.NewKeyManagementServiceClient(cc)
}

type CreateKeyRingRequest      = kmspb.CreateKeyRingRequest
type CreateCryptoKeyRequest    = kmspb.CreateCryptoKeyRequest
// ... all request types used by the gateway file
```

This pattern means the generated gateway file compiles against the canonical
`cloud.google.com/go` proto types without needing to regenerate `pb.go` files,
and there is no risk of type conflicts across the module graph.

---

## 8. Testing Architecture

All tests in `gcp-emulator` are integration tests that exercise the real
service implementations. No mocks.

### Test helpers (`testhelper_test.go`)

Three helper functions are shared across all test files:

**`setupEmulator(t)`** — Creates an in-process gRPC server, registers all four
services, and starts two listeners: a `bufconn` listener for gRPC tests (no
network) and a real ephemeral TCP port for the REST gateway:

```go
func setupEmulator(t *testing.T) (lis *bufconn.Listener, grpcAddr string, cleanup func()) {
    lis = bufconn.Listen(1024 * 1024)
    grpcSrv := grpc.NewServer()

    os.Setenv("IAM_MODE", "off")

    iam.Register(grpcSrv)
    sm.Register(grpcSrv)
    kms.Register(grpcSrv)
    eventarc.Register(grpcSrv)
    reflection.Register(grpcSrv)

    go grpcSrv.Serve(lis)

    // Real TCP port for gateway (gateway cannot use bufconn).
    tcpLis, _ := net.Listen("tcp", "localhost:0")
    grpcAddr = tcpLis.Addr().String()
    go grpcSrv.Serve(tcpLis)

    // ...
}
```

Note: `reflection.Register` is called here by the test helper, not inside any
individual `Register()` — consistent with production usage.

**`dialEmulator(t, lis)`** — Creates a gRPC client connection over the
`bufconn` listener. Uses `passthrough://bufnet` with a custom context dialer
so no TCP socket is created.

**`startGateway(t, grpcAddr)`** — Creates the unified `gateway.Gateway`
against the real TCP address and wraps it in `httptest.NewServer`. Returns
the base URL (e.g. `http://127.0.0.1:PORT`).

### Test files

| File                      | What it covers                                                   |
|---------------------------|------------------------------------------------------------------|
| `integration_test.go`     | gRPC CRUD for SM, KMS, Eventarc, IAM; cross-service; error cases |
| `rest_integration_test.go`| REST API via gateway for SM, KMS, Eventarc; 404/400 error codes  |
| `smoke_test.go`           | All four services on one port; gateway routing to all three REST paths; health endpoints |

Tests are organized as top-level `Test*` functions with `t.Run` subtests for
individual operations. Each top-level test calls `setupEmulator` independently
so tests are isolated and can run in parallel.

---

## 9. Adding a New Emulator

Follow these steps to add a new GCP service emulator to `gcp-emulator`.

### Step 1: Expose Register() at module root

In the new emulator repo, create a file at the module root (e.g.
`register.go`):

```go
package myservice

import (
    "google.golang.org/grpc"
    mypb "cloud.google.com/go/myservice/apiv1/myservicepb"
    "github.com/blackwell-systems/gcp-myservice-emulator/internal/server"
)

// Register adds the MyService gRPC service to grpcSrv.
// IAM enforcement is configured via IAM_MODE and IAM_EMULATOR_HOST.
// It does not start a listener — the caller owns the grpc.Server lifecycle.
func Register(grpcSrv *grpc.Server, opts ...Option) error {
    srv, err := server.NewServer()
    if err != nil {
        return err
    }
    mypb.RegisterMyServiceServer(grpcSrv, srv)
    // Do NOT call reflection.Register here.
    return nil
}
```

### Step 2: Expose NewGatewayHandler() at module root

```go
// gateway.go
package myservice

import (
    "net/http"
    "github.com/blackwell-systems/gcp-myservice-emulator/internal/gateway"
)

// NewGatewayHandler returns an http.Handler that proxies REST requests
// to the MyService gRPC service at grpcAddr via grpc-gateway v2.
func NewGatewayHandler(grpcAddr string) (http.Handler, error) {
    srv, err := gateway.NewServer(grpcAddr)
    if err != nil {
        return nil, err
    }
    return srv.Handler(), nil
}
```

### Step 3: Do NOT call reflection.Register inside Register()

`gcp-emulator` calls `reflection.Register(grpcSrv)` once after creating the
server. If individual emulators also called it, the server would panic at
startup with a duplicate service registration error.

### Step 4: Add to gcp-emulator's main.go

```go
import (
    myservice "github.com/blackwell-systems/gcp-myservice-emulator"
)

// Inside run():
if err := myservice.Register(grpcSrv); err != nil {
    return fmt.Errorf("myservice: %w", err)
}
```

Update the `require` block in `go.mod`:

```
github.com/blackwell-systems/gcp-myservice-emulator v0.1.0
```

### Step 5: Add gateway handler to internal/gateway/gateway.go

Add the handler field to the `Gateway` struct and construct it in `New()`:

```go
type Gateway struct {
    // ...existing fields...
    myserviceHandler http.Handler
}

func New(grpcAddr string) (*Gateway, error) {
    // ...existing handlers...
    msH, err := myservice.NewGatewayHandler(grpcAddr)
    if err != nil {
        return nil, fmt.Errorf("gateway: myservice: %w", err)
    }
    return &Gateway{
        // ...existing fields...
        myserviceHandler: msH,
    }, nil
}
```

Add a routing rule in `ServeHTTP`. Add it **before** the Secret Manager
fallback. Choose a path substring unique to the new service:

```go
if strings.Contains(path, "/myResources") {
    g.myserviceHandler.ServeHTTP(w, r)
    return
}
```

Update the package comment at the top of `gateway.go` to document the new
route.

### Step 6: Add a replace directive for local development

While the new emulator is not yet published, add a `replace` directive to
`gcp-emulator/go.mod` so the local checkout is used:

```
replace github.com/blackwell-systems/gcp-myservice-emulator => ../gcp-myservice-emulator
```

Remove the directive and update the version pin before publishing.

### Step 7: Add integration tests

Add a `TestGRPC_MyService` function to `integration_test.go` and a
`TestREST_MyService` function to `rest_integration_test.go`. Both should use
`setupEmulator` and cover at minimum: create, get, list, and not-found error.
