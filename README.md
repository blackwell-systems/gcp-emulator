# GCP Emulator

[![Blackwell Systems](https://raw.githubusercontent.com/blackwell-systems/blackwell-docs-theme/main/badge-trademark.svg)](https://github.com/blackwell-systems)
[![Go Reference](https://pkg.go.dev/badge/github.com/blackwell-systems/gcp-emulator.svg)](https://pkg.go.dev/github.com/blackwell-systems/gcp-emulator)
[![Go Version](https://img.shields.io/badge/go-1.24+-blue.svg)](https://go.dev/)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

> **Unified local emulator for Google Cloud Platform** — Secret Manager, KMS, Eventarc, and IAM enforcement in a single process.

Run your entire GCP stack locally with one command. No credentials, no network, no docker-compose juggling.

## Quick Start

```bash
# Install
go install github.com/blackwell-systems/gcp-emulator/cmd/gcp-emulator@latest

# Run (IAM off — all requests allowed)
gcp-emulator

# Run with IAM enforcement
gcp-emulator --policy policy.yaml --iam-mode strict
```

## Docker

```bash
docker run -p 9090:9090 -p 8090:8090 \
  ghcr.io/blackwell-systems/gcp-emulator:latest
```

With a policy file:

```bash
docker run -p 9090:9090 -p 8090:8090 \
  -v $(pwd)/policy.yaml:/policy.yaml \
  -e GCP_EMULATOR_POLICY_FILE=/policy.yaml \
  -e IAM_MODE=strict \
  ghcr.io/blackwell-systems/gcp-emulator:latest
```

## Services

All services share a single gRPC port. A unified REST gateway handles HTTP transcoding.

| Service | gRPC (`:9090`) | REST (`:8090`) |
|---|---|---|
| Secret Manager | `google.cloud.secretmanager.v1.SecretManagerService` | `/v1/projects/{p}/secrets/...` |
| KMS | `google.cloud.kms.v1.KeyManagementService` | `/v1/projects/{p}/locations/.../keyRings/...` |
| Eventarc | `google.cloud.eventarc.v1.Eventarc` | `/v1/projects/{p}/locations/...` |
| Eventarc Publisher | `google.cloud.eventarc.publishing.v1.Publisher` | `/v1/projects/{p}/locations/...` |
| Long-running Ops | `google.longrunning.Operations` | — |
| IAM | `google.iam.v1.IAMPolicy` | — |

Health endpoints: `GET /healthz`, `GET /readyz`

## Use with GCP Go SDK

```go
import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    secretmanager "cloud.google.com/go/secretmanager/apiv1"
    eventarc "cloud.google.com/go/eventarc/apiv1"
)

conn, _ := grpc.NewClient("localhost:9090",
    grpc.WithTransportCredentials(insecure.NewCredentials()))

smClient, _ := secretmanager.NewClient(ctx,
    option.WithGRPCConn(conn))

eaClient, _ := eventarc.NewClient(ctx,
    option.WithGRPCConn(conn))
```

Or via environment variable:

```bash
export SECRETMANAGER_EMULATOR_HOST=localhost:9090
export EVENTARC_EMULATOR_HOST=localhost:9090
```

## IAM Enforcement

Copy `policy.yaml.example` to `policy.yaml` and customize:

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

Inject the caller identity via gRPC metadata:

```
x-emulator-principal: serviceAccount:app@my-project.iam.gserviceaccount.com
```

| `--iam-mode` | Behavior |
|---|---|
| `off` (default) | All requests allowed, no policy required |
| `permissive` | Logs denials, does not block |
| `strict` | Blocks unauthorized requests with `PERMISSION_DENIED` |

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `GCP_EMULATOR_GRPC_PORT` | `9090` | gRPC port for all services |
| `GCP_EMULATOR_HTTP_PORT` | `8090` | REST gateway port |
| `GCP_EMULATOR_POLICY_FILE` | — | Path to IAM policy YAML |
| `IAM_MODE` | `off` | IAM enforcement mode |
| `IAM_TRACE` | `false` | Log every IAM decision |

## Individual Service Emulators

Need just one service? Use the standalone emulators:

| Service | Repository |
|---|---|
| Secret Manager | [`gcp-secret-manager-emulator`](https://github.com/blackwell-systems/gcp-secret-manager-emulator) |
| KMS | [`gcp-kms-emulator`](https://github.com/blackwell-systems/gcp-kms-emulator) |
| Eventarc | [`gcp-eventarc-emulator`](https://github.com/blackwell-systems/gcp-eventarc-emulator) |
| IAM | [`gcp-iam-emulator`](https://github.com/blackwell-systems/gcp-iam-emulator) |

## Architecture

```
gcp-emulator (single process)
│
├── gRPC :9090
│   ├── SecretManagerService      ← gcp-secret-manager-emulator
│   ├── KeyManagementService      ← gcp-kms-emulator
│   ├── Eventarc                  ← gcp-eventarc-emulator
│   ├── Publisher                 ← gcp-eventarc-emulator
│   ├── Operations                ← gcp-eventarc-emulator
│   └── IAMPolicy                 ← gcp-iam-emulator
│
└── REST :8090
    ├── /v1/projects/*/secrets/*          → Secret Manager
    ├── /v1/projects/*/locations/*/keyRings/* → KMS
    ├── /v1/projects/*/locations/*         → Eventarc
    └── /healthz, /readyz
```

## Migrating from gcp-iam-control-plane

If you were using the now-archived [`gcp-iam-control-plane`](https://github.com/blackwell-systems/gcp-iam-control-plane) CLI, this repo is its replacement. The old CLI orchestrated separate emulators via docker-compose; this one runs everything in a single process.

```bash
# Old
go install github.com/blackwell-systems/gcp-iam-control-plane/cmd/gcp-emulator@latest

# New
go install github.com/blackwell-systems/gcp-emulator/cmd/gcp-emulator@latest
```

The `--policy`, `--iam-mode`, `--trace`, and `--watch` flags are all preserved. No changes to `policy.yaml` format required.

## License

Apache 2.0 — see [LICENSE](LICENSE).
