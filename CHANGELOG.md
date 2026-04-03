# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/blackwell-systems/gcp-emulator/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/blackwell-systems/gcp-emulator/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/blackwell-systems/gcp-emulator/releases/tag/v0.1.0
