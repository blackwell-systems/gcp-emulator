# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-04-02

### Added

- Unified gRPC server (`:9090`) serving Secret Manager, Eventarc, and IAM in one process
- Unified REST gateway (`:8090`) routing by URL path structure (`/locations/` → Eventarc, otherwise → Secret Manager)
- IAM enforcement via `--policy` and `--iam-mode` flags (off / permissive / strict)
- Hot-reload of policy file with `--watch`
- Health endpoints: `GET /healthz`, `GET /readyz`
- Cobra CLI with `--help` and `--version`
- Dockerfile and `docker-compose.yml` for containerized use
- `policy.yaml.example` with role definitions for all included services
- Makefile with `build`, `run`, `test`, `fmt`, `fmt-check`, `docker-build`, `clean`
- Apache 2.0 license
