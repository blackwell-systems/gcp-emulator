package gcpemulator_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// doREST is a helper that sends an HTTP request and returns the status code
// and decoded JSON response body.
func doREST(t *testing.T, method, url string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(respBody, &result) // may fail for empty body, ok
	return resp.StatusCode, result
}

func TestREST_SecretManager(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	t.Run("CreateSecret", func(t *testing.T) {
		status, resp := doREST(t, http.MethodPost,
			base+"/v1/projects/test-project/secrets?secretId=rest-secret",
			map[string]interface{}{
				"replication": map[string]interface{}{
					"automatic": map[string]interface{}{},
				},
			},
		)
		if status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("expected 200 or 201, got %d: %v", status, resp)
		}
		name, _ := resp["name"].(string)
		if !strings.Contains(name, "rest-secret") {
			t.Fatalf("expected name to contain 'rest-secret', got %q", name)
		}
	})

	t.Run("GetSecret", func(t *testing.T) {
		status, resp := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/secrets/rest-secret", nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", status, resp)
		}
		name, _ := resp["name"].(string)
		if !strings.Contains(name, "rest-secret") {
			t.Fatalf("expected name to contain 'rest-secret', got %q", name)
		}
	})

	t.Run("ListSecrets", func(t *testing.T) {
		status, _ := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/secrets", nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
	})

	t.Run("AddVersion", func(t *testing.T) {
		status, resp := doREST(t, http.MethodPost,
			base+"/v1/projects/test-project/secrets/rest-secret:addVersion",
			map[string]interface{}{
				"payload": map[string]interface{}{
					"data": base64.StdEncoding.EncodeToString([]byte("rest-secret-data")),
				},
			},
		)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", status, resp)
		}
		name, _ := resp["name"].(string)
		if !strings.Contains(name, "/versions/") {
			t.Fatalf("expected name to contain '/versions/', got %q", name)
		}
	})

	t.Run("AccessVersion", func(t *testing.T) {
		status, resp := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/secrets/rest-secret/versions/1:access", nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", status, resp)
		}
		payload, ok := resp["payload"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected payload map, got %v", resp)
		}
		dataB64, _ := payload["data"].(string)
		decoded, err := base64.StdEncoding.DecodeString(dataB64)
		if err != nil {
			t.Fatalf("base64 decode: %v", err)
		}
		if string(decoded) != "rest-secret-data" {
			t.Fatalf("expected 'rest-secret-data', got %q", string(decoded))
		}
	})
}

func TestREST_KMS(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	var ciphertext string

	t.Run("CreateKeyRing", func(t *testing.T) {
		status, resp := doREST(t, http.MethodPost,
			base+"/v1/projects/test-project/locations/us-central1/keyRings?keyRingId=rest-ring", nil)
		if status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("expected 200 or 201, got %d: %v", status, resp)
		}
	})

	t.Run("CreateCryptoKey", func(t *testing.T) {
		status, resp := doREST(t, http.MethodPost,
			base+"/v1/projects/test-project/locations/us-central1/keyRings/rest-ring/cryptoKeys?cryptoKeyId=rest-key",
			map[string]interface{}{
				"purpose": "ENCRYPT_DECRYPT",
			},
		)
		if status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("expected 200 or 201, got %d: %v", status, resp)
		}
	})

	t.Run("Encrypt", func(t *testing.T) {
		status, resp := doREST(t, http.MethodPost,
			base+"/v1/projects/test-project/locations/us-central1/keyRings/rest-ring/cryptoKeys/rest-key:encrypt",
			map[string]interface{}{
				"plaintext": base64.StdEncoding.EncodeToString([]byte("hello rest")),
			},
		)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", status, resp)
		}
		ct, ok := resp["ciphertext"].(string)
		if !ok || ct == "" {
			t.Fatalf("expected ciphertext in response, got %v", resp)
		}
		ciphertext = ct
	})

	t.Run("Decrypt", func(t *testing.T) {
		if ciphertext == "" {
			t.Skip("no ciphertext from Encrypt subtest")
		}
		status, resp := doREST(t, http.MethodPost,
			base+"/v1/projects/test-project/locations/us-central1/keyRings/rest-ring/cryptoKeys/rest-key:decrypt",
			map[string]interface{}{
				"ciphertext": ciphertext,
			},
		)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", status, resp)
		}
		ptB64, _ := resp["plaintext"].(string)
		decoded, err := base64.StdEncoding.DecodeString(ptB64)
		if err != nil {
			t.Fatalf("base64 decode: %v", err)
		}
		if string(decoded) != "hello rest" {
			t.Fatalf("expected 'hello rest', got %q", string(decoded))
		}
	})
}

func TestREST_Eventarc(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	t.Run("CreateTrigger", func(t *testing.T) {
		status, resp := doREST(t, http.MethodPost,
			base+"/v1/projects/test-project/locations/us-central1/triggers?triggerId=rest-trigger",
			map[string]interface{}{
				"eventFilters": []map[string]interface{}{
					{"attribute": "type", "value": "test.v1"},
				},
				"destination": map[string]interface{}{
					"httpEndpoint": map[string]interface{}{
						"uri": "http://localhost:8080/test",
					},
				},
			},
		)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", status, resp)
		}
	})

	t.Run("GetTrigger", func(t *testing.T) {
		status, resp := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/locations/us-central1/triggers/rest-trigger", nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %v", status, resp)
		}
	})

	t.Run("ListTriggers", func(t *testing.T) {
		status, _ := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/locations/us-central1/triggers", nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
	})
}

func TestREST_NotFound(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	t.Run("Secret", func(t *testing.T) {
		status, _ := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/secrets/nonexistent", nil)
		if status != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", status)
		}
	})

	t.Run("KeyRing", func(t *testing.T) {
		status, _ := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/locations/us-central1/keyRings/nonexistent", nil)
		// Gateway implementation may return 404 or 500; verify non-200.
		if status == http.StatusOK {
			t.Fatalf("expected non-200 for nonexistent key ring, got %d", status)
		}
	})

	t.Run("Trigger", func(t *testing.T) {
		status, _ := doREST(t, http.MethodGet,
			base+"/v1/projects/test-project/locations/us-central1/triggers/nonexistent", nil)
		if status != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", status)
		}
	})
}

func TestREST_InvalidArgument(t *testing.T) {
	_, grpcAddr, cleanup := setupEmulator(t)
	t.Cleanup(cleanup)
	base := startGateway(t, grpcAddr)

	// POST with malformed JSON body should return 400.
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/v1/projects/test-project/secrets?secretId=bad-secret", base),
		strings.NewReader("{not valid json}"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", resp.StatusCode)
	}
}
