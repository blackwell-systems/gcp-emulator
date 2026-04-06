# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.2] - 2026-04-05

### Changed

- Bumped all emulator dependencies to latest:
  - gcp-eventarc-emulator v0.1.2 → v0.2.3
  - gcp-kms-emulator v0.5.0 → v0.8.1
  - gcp-secret-manager-emulator v1.6.0 → v1.7.1
  - gcp-iam-emulator v0.10.0 → v0.10.1
  - gcp-emulator-auth v0.3.0 → v0.4.1

### Fixed

- Added missing `gcp-kms-emulator` to Dockerfile `COPY` steps
- Fixed release workflow to authenticate sibling repo clones with `GITHUB_TOKEN`
- Added `go work init` in Dockerfile to use local module sources (avoids private repo auth in Docker build)

## [0.2.1] - 2026-04-03

### Added
- `docs/ARCHITECTURE.md` — composition pattern, REST gateway routing, IAM enforcement, testing architecture, and guide for adding new emulators
- README: migration section for users coming from `gcp-iam-control-plane`

## [0.2.0] - 2026-04-03

### Added
- 41 integration tests covering gRPC and REST for all 4 services (SM, KMS, Eventarc, IAM)
- Smoke tests for `/healthz`, `/readyz`, and cross-service coexistence
- REST integration tests verifying HTTP status codes and response format
- IAM REST gateway routing in unified gateway (`:setIamPolicy`, `:getIamPolicy`, `:testIamPermissions`)

### Changed
- All 4 service REST gateways now use grpc-gateway v2 (IAM migrated from hand-rolled HTTP)
- Bumped gcp-iam-emulator to v0.10.0

### Fixed
- Duplicate `reflection.Register` crash when composing all emulators on one gRPC server
- REST gateway now uses grpc-gateway v2 from upstream SM, KMS, and IAM emulators (correct HTTP status codes, GCP error format, malformed JSON rejection)

## [0.1.0] - 2026-04-02

### Added

- Unified gRPC server (`:9090`) serving Secret Manager, KMS, Eventarc, and IAM in one process
- Unified REST gateway (`:8090`) routing by URL path structure (`/locations/` → Eventarc, otherwise → Secret Manager)
- IAM enforcement via `--policy` and `--iam-mode` flags (off / permissive / strict)
- Hot-reload of policy file with `--watch`
- Health endpoints: `GET /healthz`, `GET /readyz`
- Cobra CLI with `--help` and `--version`
- Dockerfile and `docker-compose.yml` for containerized use
- `policy.yaml.example` with role definitions for all included services
- Makefile with `build`, `run`, `test`, `fmt`, `fmt-check`, `docker-build`, `clean`
- Apache 2.0 license

[Unreleased]: https://github.com/blackwell-systems/gcp-emulator/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/blackwell-systems/gcp-emulator/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/blackwell-systems/gcp-emulator/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/blackwell-systems/gcp-emulator/releases/tag/v0.1.0
