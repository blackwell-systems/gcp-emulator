package gcpemulator_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	iampb "google.golang.org/genproto/googleapis/iam/v1"
)

func TestHealth_Healthz(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	// Verify exact JSON structure
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	if result["status"] != "ok" {
		t.Fatalf("expected {\"status\":\"ok\"}, got %s", string(body))
	}
}

func TestHealth_Readyz(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	if result["status"] != "ok" {
		t.Fatalf("expected {\"status\":\"ok\"}, got %s", string(body))
	}
}

func TestSmoke_AllServicesOnePort(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)

	smClient := secretmanagerpb.NewSecretManagerServiceClient(conn)
	kmsClient := kmspb.NewKeyManagementServiceClient(conn)
	eaClient := eventarcpb.NewEventarcClient(conn)
	iamClient := iampb.NewIAMPolicyClient(conn)

	ctx := context.Background()

	_, err := smClient.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
		Parent: "projects/test-project",
	})
	if err != nil {
		t.Fatalf("ListSecrets error: %v", err)
	}

	_, err = kmsClient.ListKeyRings(ctx, &kmspb.ListKeyRingsRequest{
		Parent: "projects/test-project/locations/us-central1",
	})
	if err != nil {
		t.Fatalf("ListKeyRings error: %v", err)
	}

	_, err = eaClient.ListTriggers(ctx, &eventarcpb.ListTriggersRequest{
		Parent: "projects/test-project/locations/us-central1",
	})
	if err != nil {
		t.Fatalf("ListTriggers error: %v", err)
	}

	_, err = iamClient.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Resource: "projects/test-project",
	})
	if err != nil {
		t.Fatalf("GetIamPolicy error: %v", err)
	}
}

func TestSmoke_GatewayRouting(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	routes := []struct {
		name string
		path string
	}{
		{"SecretManager", "/v1/projects/test-project/secrets"},
		{"KMS", "/v1/projects/test-project/locations/us-central1/keyRings"},
		{"Eventarc", "/v1/projects/test-project/locations/us-central1/triggers"},
	}

	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			resp, err := http.Get(base + r.path)
			if err != nil {
				t.Fatalf("GET %s error: %v", r.path, err)
			}
			resp.Body.Close()

			if resp.StatusCode != 200 {
				t.Fatalf("GET %s: expected status 200, got %d", r.path, resp.StatusCode)
			}
		})
	}
}
