// Package gcpemulator provides the unified local emulator for Google Cloud Platform.
//
// # Overview
//
// gcp-emulator runs Secret Manager, KMS, Eventarc, and IAM enforcement in a single
// process. All services share a gRPC port (default :9090); a unified REST gateway
// (default :8090) transcodes HTTP/JSON to gRPC.
//
// No GCP credentials or network access required. All state is in-memory.
//
// # Quick Start
//
//	go install github.com/blackwell-systems/gcp-emulator/cmd/gcp-emulator@latest
//	gcp-emulator
//
// # Docker
//
//	docker run -p 9090:9090 -p 8090:8090 ghcr.io/blackwell-systems/gcp-emulator:latest
//
// # With IAM Enforcement
//
//	gcp-emulator --policy policy.yaml --iam-mode strict
//
// # Service Endpoints (gRPC :9090)
//
//	google.cloud.secretmanager.v1.SecretManagerService
//	google.cloud.kms.v1.KeyManagementService
//	google.cloud.eventarc.v1.Eventarc
//	google.cloud.eventarc.publishing.v1.Publisher
//	google.longrunning.Operations
//	google.iam.v1.IAMPolicy
//
// # REST Gateway (:8090)
//
//	/v1/projects/{project}/secrets/*              → Secret Manager
//	/v1/projects/{project}/locations/*/keyRings/* → KMS
//	/v1/projects/{project}/locations/*            → Eventarc
//	/healthz, /readyz                         → health endpoints
//
// # Environment Variables
//
//	GCP_EMULATOR_GRPC_PORT   gRPC port (default: 9090)
//	GCP_EMULATOR_HTTP_PORT   REST gateway port (default: 8090)
//	GCP_EMULATOR_POLICY_FILE Path to IAM policy YAML file
//	IAM_MODE                 IAM enforcement: off, permissive, strict (default: off)
//	IAM_TRACE                Log IAM decisions: true/false
//
// # Individual Service Emulators
//
// Each service is also available as a standalone emulator:
//
//	github.com/blackwell-systems/gcp-secret-manager-emulator
//	github.com/blackwell-systems/gcp-kms-emulator
//	github.com/blackwell-systems/gcp-eventarc-emulator
//	github.com/blackwell-systems/gcp-iam-emulator
package gcpemulator
