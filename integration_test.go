package gcpemulator_test

import (
	"context"
	"strings"
	"testing"

	eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	longrunningpb "cloud.google.com/go/longrunning/autogen/longrunningpb"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	iampb "google.golang.org/genproto/googleapis/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// unpackTrigger extracts a Trigger from a completed LRO.
func unpackTrigger(t *testing.T, op *longrunningpb.Operation) *eventarcpb.Trigger {
	t.Helper()
	if !op.Done {
		t.Fatal("expected operation to be DONE")
	}
	var trigger eventarcpb.Trigger
	if err := op.GetResponse().UnmarshalTo(&trigger); err != nil {
		t.Fatalf("UnmarshalTo(Trigger) error: %v", err)
	}
	return &trigger
}

func TestGRPC_SecretManager(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	client := secretmanagerpb.NewSecretManagerServiceClient(conn)
	ctx := context.Background()

	var secretName string
	var versionName string

	t.Run("Create", func(t *testing.T) {
		resp, err := client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
			Parent:   "projects/test-project",
			SecretId: "test-secret",
			Secret: &secretmanagerpb.Secret{
				Replication: &secretmanagerpb.Replication{
					Replication: &secretmanagerpb.Replication_Automatic_{
						Automatic: &secretmanagerpb.Replication_Automatic{},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("CreateSecret error: %v", err)
		}
		if resp.Name != "projects/test-project/secrets/test-secret" {
			t.Fatalf("unexpected name: %s", resp.Name)
		}
		secretName = resp.Name
	})

	t.Run("Get", func(t *testing.T) {
		resp, err := client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{
			Name: secretName,
		})
		if err != nil {
			t.Fatalf("GetSecret error: %v", err)
		}
		if resp.Name != secretName {
			t.Fatalf("unexpected name: got %s, want %s", resp.Name, secretName)
		}
	})

	t.Run("List", func(t *testing.T) {
		resp, err := client.ListSecrets(ctx, &secretmanagerpb.ListSecretsRequest{
			Parent: "projects/test-project",
		})
		if err != nil {
			t.Fatalf("ListSecrets error: %v", err)
		}
		if len(resp.Secrets) < 1 {
			t.Fatal("expected at least 1 secret")
		}
	})

	t.Run("AddVersion", func(t *testing.T) {
		resp, err := client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
			Parent: secretName,
			Payload: &secretmanagerpb.SecretPayload{
				Data: []byte("my-secret-data"),
			},
		})
		if err != nil {
			t.Fatalf("AddSecretVersion error: %v", err)
		}
		if !strings.Contains(resp.Name, "/versions/") {
			t.Fatalf("version name missing /versions/: %s", resp.Name)
		}
		versionName = resp.Name
	})

	t.Run("AccessVersion", func(t *testing.T) {
		resp, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
			Name: versionName,
		})
		if err != nil {
			t.Fatalf("AccessSecretVersion error: %v", err)
		}
		if string(resp.Payload.Data) != "my-secret-data" {
			t.Fatalf("unexpected payload: got %q, want %q", string(resp.Payload.Data), "my-secret-data")
		}
	})
}

func TestGRPC_KMS(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	client := kmspb.NewKeyManagementServiceClient(conn)
	ctx := context.Background()

	parent := "projects/test-project/locations/us-central1"
	var keyRingName string
	var cryptoKeyName string
	var ciphertext []byte

	t.Run("CreateKeyRing", func(t *testing.T) {
		resp, err := client.CreateKeyRing(ctx, &kmspb.CreateKeyRingRequest{
			Parent:    parent,
			KeyRingId: "test-ring",
		})
		if err != nil {
			t.Fatalf("CreateKeyRing error: %v", err)
		}
		expected := parent + "/keyRings/test-ring"
		if resp.Name != expected {
			t.Fatalf("unexpected name: got %s, want %s", resp.Name, expected)
		}
		keyRingName = resp.Name
	})

	t.Run("CreateCryptoKey", func(t *testing.T) {
		resp, err := client.CreateCryptoKey(ctx, &kmspb.CreateCryptoKeyRequest{
			Parent:      keyRingName,
			CryptoKeyId: "test-key",
			CryptoKey: &kmspb.CryptoKey{
				Purpose: kmspb.CryptoKey_ENCRYPT_DECRYPT,
			},
		})
		if err != nil {
			t.Fatalf("CreateCryptoKey error: %v", err)
		}
		if !strings.Contains(resp.Name, "/cryptoKeys/test-key") {
			t.Fatalf("unexpected name: %s", resp.Name)
		}
		cryptoKeyName = resp.Name
	})

	t.Run("Encrypt", func(t *testing.T) {
		resp, err := client.Encrypt(ctx, &kmspb.EncryptRequest{
			Name:      cryptoKeyName,
			Plaintext: []byte("hello world"),
		})
		if err != nil {
			t.Fatalf("Encrypt error: %v", err)
		}
		if len(resp.Ciphertext) == 0 {
			t.Fatal("expected non-empty ciphertext")
		}
		ciphertext = resp.Ciphertext
	})

	t.Run("Decrypt", func(t *testing.T) {
		resp, err := client.Decrypt(ctx, &kmspb.DecryptRequest{
			Name:       cryptoKeyName,
			Ciphertext: ciphertext,
		})
		if err != nil {
			t.Fatalf("Decrypt error: %v", err)
		}
		if string(resp.Plaintext) != "hello world" {
			t.Fatalf("unexpected plaintext: got %q, want %q", string(resp.Plaintext), "hello world")
		}
	})

	t.Run("ListCryptoKeys", func(t *testing.T) {
		resp, err := client.ListCryptoKeys(ctx, &kmspb.ListCryptoKeysRequest{
			Parent: keyRingName,
		})
		if err != nil {
			t.Fatalf("ListCryptoKeys error: %v", err)
		}
		found := false
		for _, k := range resp.CryptoKeys {
			if strings.Contains(k.Name, "/cryptoKeys/test-key") {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("test-key not found in ListCryptoKeys response")
		}
	})

	t.Run("DestroyCryptoKeyVersion", func(t *testing.T) {
		resp, err := client.DestroyCryptoKeyVersion(ctx, &kmspb.DestroyCryptoKeyVersionRequest{
			Name: cryptoKeyName + "/cryptoKeyVersions/1",
		})
		if err != nil {
			t.Fatalf("DestroyCryptoKeyVersion error: %v", err)
		}
		if resp.State != kmspb.CryptoKeyVersion_DESTROY_SCHEDULED {
			t.Fatalf("unexpected state: got %v, want DESTROY_SCHEDULED", resp.State)
		}
	})
}

func TestGRPC_Eventarc(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	client := eventarcpb.NewEventarcClient(conn)
	ctx := context.Background()

	parent := "projects/test-project/locations/us-central1"
	var triggerName string

	t.Run("CreateTrigger", func(t *testing.T) {
		op, err := client.CreateTrigger(ctx, &eventarcpb.CreateTriggerRequest{
			Parent:    parent,
			TriggerId: "test-trigger",
			Trigger: &eventarcpb.Trigger{
				EventFilters: []*eventarcpb.EventFilter{
					{Attribute: "type", Value: "test.v1"},
				},
				Destination: &eventarcpb.Destination{
					Descriptor_: &eventarcpb.Destination_HttpEndpoint{
						HttpEndpoint: &eventarcpb.HttpEndpoint{
							Uri: "http://localhost:8080/test",
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("CreateTrigger error: %v", err)
		}
		trigger := unpackTrigger(t, op)
		expected := parent + "/triggers/test-trigger"
		if trigger.Name != expected {
			t.Fatalf("unexpected name: got %s, want %s", trigger.Name, expected)
		}
		triggerName = trigger.Name
	})

	t.Run("GetTrigger", func(t *testing.T) {
		resp, err := client.GetTrigger(ctx, &eventarcpb.GetTriggerRequest{
			Name: triggerName,
		})
		if err != nil {
			t.Fatalf("GetTrigger error: %v", err)
		}
		if resp.Name != triggerName {
			t.Fatalf("unexpected name: got %s, want %s", resp.Name, triggerName)
		}
	})

	t.Run("ListTriggers", func(t *testing.T) {
		resp, err := client.ListTriggers(ctx, &eventarcpb.ListTriggersRequest{
			Parent: parent,
		})
		if err != nil {
			t.Fatalf("ListTriggers error: %v", err)
		}
		found := false
		for _, tr := range resp.Triggers {
			if tr.Name == triggerName {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("test-trigger not found in ListTriggers response")
		}
	})
}

func TestGRPC_IAM(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	client := iampb.NewIAMPolicyClient(conn)
	ctx := context.Background()

	resource := "projects/test-project"

	t.Run("SetPolicy", func(t *testing.T) {
		_, err := client.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
			Resource: resource,
			Policy: &iampb.Policy{
				Bindings: []*iampb.Binding{
					{
						Role:    "roles/viewer",
						Members: []string{"user:test@example.com"},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("SetIamPolicy error: %v", err)
		}
	})

	t.Run("GetPolicy", func(t *testing.T) {
		resp, err := client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
			Resource: resource,
		})
		if err != nil {
			t.Fatalf("GetIamPolicy error: %v", err)
		}
		if len(resp.Bindings) < 1 {
			t.Fatal("expected at least 1 binding")
		}
		found := false
		for _, b := range resp.Bindings {
			if b.Role == "roles/viewer" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("roles/viewer binding not found")
		}
	})

	t.Run("TestPermissions", func(t *testing.T) {
		_, err := client.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
			Resource:    resource,
			Permissions: []string{"secretmanager.secrets.get"},
		})
		if err != nil {
			t.Fatalf("TestIamPermissions error: %v", err)
		}
	})
}

func TestGRPC_CrossService(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	ctx := context.Background()

	smClient := secretmanagerpb.NewSecretManagerServiceClient(conn)
	kmsClient := kmspb.NewKeyManagementServiceClient(conn)
	eaClient := eventarcpb.NewEventarcClient(conn)
	iamClient := iampb.NewIAMPolicyClient(conn)

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

func TestGRPC_NotFound(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	ctx := context.Background()

	t.Run("Secret", func(t *testing.T) {
		client := secretmanagerpb.NewSecretManagerServiceClient(conn)
		_, err := client.GetSecret(ctx, &secretmanagerpb.GetSecretRequest{
			Name: "projects/test-project/secrets/nonexistent",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})

	t.Run("CryptoKey", func(t *testing.T) {
		client := kmspb.NewKeyManagementServiceClient(conn)
		_, err := client.GetCryptoKey(ctx, &kmspb.GetCryptoKeyRequest{
			Name: "projects/test-project/locations/us-central1/keyRings/x/cryptoKeys/y",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})

	t.Run("Trigger", func(t *testing.T) {
		client := eventarcpb.NewEventarcClient(conn)
		_, err := client.GetTrigger(ctx, &eventarcpb.GetTriggerRequest{
			Name: "projects/test-project/locations/us-central1/triggers/nonexistent",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.NotFound {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})
}

func TestGRPC_AlreadyExists(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	ctx := context.Background()

	t.Run("Secret", func(t *testing.T) {
		client := secretmanagerpb.NewSecretManagerServiceClient(conn)
		req := &secretmanagerpb.CreateSecretRequest{
			Parent:   "projects/test-project",
			SecretId: "dup-secret",
			Secret: &secretmanagerpb.Secret{
				Replication: &secretmanagerpb.Replication{
					Replication: &secretmanagerpb.Replication_Automatic_{
						Automatic: &secretmanagerpb.Replication_Automatic{},
					},
				},
			},
		}
		_, err := client.CreateSecret(ctx, req)
		if err != nil {
			t.Fatalf("first CreateSecret error: %v", err)
		}
		_, err = client.CreateSecret(ctx, req)
		if err == nil {
			t.Fatal("expected error on duplicate, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.AlreadyExists {
			t.Fatalf("expected AlreadyExists, got %v", err)
		}
	})

	t.Run("KeyRing", func(t *testing.T) {
		client := kmspb.NewKeyManagementServiceClient(conn)
		req := &kmspb.CreateKeyRingRequest{
			Parent:    "projects/test-project/locations/us-central1",
			KeyRingId: "dup-ring",
		}
		_, err := client.CreateKeyRing(ctx, req)
		if err != nil {
			t.Fatalf("first CreateKeyRing error: %v", err)
		}
		_, err = client.CreateKeyRing(ctx, req)
		if err == nil {
			t.Fatal("expected error on duplicate, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.AlreadyExists {
			t.Fatalf("expected AlreadyExists, got %v", err)
		}
	})
}

func TestGRPC_InvalidArgument(t *testing.T) {
	lis, _, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	conn := dialEmulator(t, lis)
	ctx := context.Background()

	t.Run("Secret", func(t *testing.T) {
		client := secretmanagerpb.NewSecretManagerServiceClient(conn)
		_, err := client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
			Parent:   "",
			SecretId: "test",
			Secret:   &secretmanagerpb.Secret{},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("KeyRing", func(t *testing.T) {
		client := kmspb.NewKeyManagementServiceClient(conn)
		_, err := client.CreateKeyRing(ctx, &kmspb.CreateKeyRingRequest{
			Parent:    "",
			KeyRingId: "test",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("Trigger", func(t *testing.T) {
		client := eventarcpb.NewEventarcClient(conn)
		_, err := client.CreateTrigger(ctx, &eventarcpb.CreateTriggerRequest{
			Parent:    "",
			TriggerId: "test",
			Trigger:   &eventarcpb.Trigger{},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if s, ok := status.FromError(err); !ok || s.Code() != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})
}
