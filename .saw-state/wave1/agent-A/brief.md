---
saw_name: [SAW:wave1:agent-A] ## Test Helper + gRPC Integration Tests
---

# Agent A Brief - Wave 1

**IMPL Doc:** /Users/dayna.blackwell/code/gcp-emulator/docs/IMPL/IMPL-integration-tests.yaml

## Files Owned

- `testhelper_test.go`
- `integration_test.go`


## Task

## Test Helper + gRPC Integration Tests

Create two files in the repo root, both with package gcpemulator_test.

### File 1: testhelper_test.go — shared test infrastructure

This file provides three helper functions used by all test files.

**Imports needed:**
```go
import (
    "context"
    "net"
    "os"
    "testing"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "google.golang.org/grpc/test/bufconn"

    eventarc "github.com/blackwell-systems/gcp-eventarc-emulator"
    iam "github.com/blackwell-systems/gcp-iam-emulator"
    kms "github.com/blackwell-systems/gcp-kms-emulator"
    sm "github.com/blackwell-systems/gcp-secret-manager-emulator"

    "github.com/blackwell-systems/gcp-emulator/internal/gateway"

    "net/http/httptest"
)
```

**func setupEmulator(t *testing.T) (*bufconn.Listener, string, func())**
- Create bufconn.Listen(1024 * 1024)
- Create grpc.NewServer()
- os.Setenv("IAM_MODE", "off")
- Register all 4 services: iam.Register(grpcSrv), sm.Register(grpcSrv),
  kms.Register(grpcSrv), eventarc.Register(grpcSrv)
- Fatal on any Register error
- Start grpcSrv.Serve(lis) in goroutine for bufconn
- Also: net.Listen("tcp", "localhost:0") for a real TCP listener, then
  start a second goroutine: grpcSrv.Serve(tcpLis). This gives the REST
  gateway a real address to dial. Extract the addr via tcpLis.Addr().String()
- Return (bufconnLis, tcpAddr, cleanup) where cleanup stops the server
  and closes both listeners

**func dialEmulator(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn**
- grpc.NewClient("passthrough://bufnet",
    grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
        return lis.DialContext(ctx)
    }),
    grpc.WithTransportCredentials(insecure.NewCredentials()))
- t.Cleanup(conn.Close)
- Return conn

**func startGateway(t *testing.T, grpcAddr string) string**
- gw, err := gateway.New(grpcAddr) — fatal on error
- ts := httptest.NewServer(gw)
- t.Cleanup(ts.Close)
- Return ts.URL

### File 2: integration_test.go — gRPC integration tests

**Imports needed:**
```go
import (
    "context"
    "testing"

    secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
    kmspb "cloud.google.com/go/kms/apiv1/kmspb"
    eventarcpb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
    longrunningpb "cloud.google.com/go/longrunning/autogen/longrunningpb"
    iampb "google.golang.org/genproto/googleapis/iam/v1"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)
```

**Local helper — unpackTrigger(t, op):**
```go
func unpackTrigger(t *testing.T, op *longrunningpb.Operation) *eventarcpb.Trigger {
    t.Helper()
    if !op.Done { t.Fatal("expected operation to be DONE") }
    var trigger eventarcpb.Trigger
    if err := op.GetResponse().UnmarshalTo(&trigger); err != nil {
        t.Fatalf("UnmarshalTo(Trigger) error: %v", err)
    }
    return &trigger
}
```

**TestGRPC_SecretManager** with subtests:
- Setup: lis, _, cleanup := setupEmulator(t); conn := dialEmulator(t, lis)
- client := secretmanagerpb.NewSecretManagerServiceClient(conn)
- "Create": CreateSecret(parent="projects/test-project", secretId="test-secret",
  secret with Replication.Automatic). Verify name = "projects/test-project/secrets/test-secret"
- "Get": GetSecret by name, verify name matches
- "List": ListSecrets(parent), verify at least 1 secret returned
- "AddVersion": AddSecretVersion(parent=secret name, payload data=[]byte("my-secret-data"))
  Verify version name contains "/versions/"
- "AccessVersion": AccessSecretVersion(name=version name from AddVersion)
  Verify payload.data = []byte("my-secret-data")

**TestGRPC_KMS** with subtests:
- Setup: lis, _, cleanup := setupEmulator(t); conn := dialEmulator(t, lis)
- client := kmspb.NewKeyManagementServiceClient(conn)
- parent := "projects/test-project/locations/us-central1"
- "CreateKeyRing": CreateKeyRing(parent, keyRingId="test-ring")
  Verify name = parent + "/keyRings/test-ring"
- "CreateCryptoKey": CreateCryptoKey(parent=keyRing name, cryptoKeyId="test-key",
  purpose=ENCRYPT_DECRYPT). Verify name contains "/cryptoKeys/test-key"
- "Encrypt": Encrypt(name=cryptoKey name, plaintext=[]byte("hello world"))
  Store ciphertext. Verify ciphertext is non-empty.
- "Decrypt": Decrypt(name=cryptoKey name, ciphertext from Encrypt)
  Verify plaintext = []byte("hello world")
- "ListCryptoKeys": ListCryptoKeys(parent=keyRing name)
  Verify our key appears in the list
- "DestroyCryptoKeyVersion": DestroyCryptoKeyVersion(name=cryptoKey name + "/cryptoKeyVersions/1")
  Verify state is DESTROY_SCHEDULED

**TestGRPC_Eventarc** with subtests:
- Setup: lis, _, cleanup := setupEmulator(t); conn := dialEmulator(t, lis)
- client := eventarcpb.NewEventarcClient(conn)
- parent := "projects/test-project/locations/us-central1"
- "CreateTrigger": CreateTrigger(parent, triggerId="test-trigger", trigger with
  EventFilters=[{attribute:"type", value:"test.v1"}],
  Destination.HttpEndpoint.Uri="http://localhost:8080/test")
  Unpack LRO, verify trigger name = parent + "/triggers/test-trigger"
- "GetTrigger": GetTrigger(name), verify name matches
- "ListTriggers": ListTriggers(parent), verify trigger appears

**TestGRPC_IAM** with subtests:
- Setup: lis, _, cleanup := setupEmulator(t); conn := dialEmulator(t, lis)
- client := iampb.NewIAMPolicyClient(conn)
- resource := "projects/test-project"
- "SetPolicy": SetIamPolicy(resource, policy with Bindings=[{Role:"roles/viewer",
  Members:["user:test@example.com"]}]). Verify no error.
- "GetPolicy": GetIamPolicy(resource). Verify at least 1 binding with role "roles/viewer"
- "TestPermissions": TestIamPermissions(resource, permissions=["secretmanager.secrets.get"])
  Verify no error (response permissions may be empty with IAM mode off)

**TestGRPC_CrossService**:
- Single setupEmulator + dialEmulator connection
- Create all 4 clients from same conn
- Call ListSecrets, ListKeyRings, ListTriggers, GetIamPolicy
- Verify all return without error

**TestGRPC_NotFound** with subtests:
- "Secret": GetSecret("projects/test-project/secrets/nonexistent"), expect codes.NotFound
- "CryptoKey": GetCryptoKey("projects/test-project/locations/us-central1/keyRings/x/cryptoKeys/y"),
  expect codes.NotFound
- "Trigger": GetTrigger("projects/test-project/locations/us-central1/triggers/nonexistent"),
  expect codes.NotFound

**TestGRPC_AlreadyExists** with subtests:
- "Secret": Create secret "dup-secret", create again, expect codes.AlreadyExists
- "KeyRing": Create keyring "dup-ring", create again, expect codes.AlreadyExists

**TestGRPC_InvalidArgument** with subtests:
- "Secret": CreateSecret with empty parent, expect codes.InvalidArgument
- "KeyRing": CreateKeyRing with empty parent, expect codes.InvalidArgument
- "Trigger": CreateTrigger with empty parent, expect codes.InvalidArgument

### Verification gate
```
go test -v -run "TestGRPC" -count=1 .
```

### Constraints
- Package: gcpemulator_test (external test package)
- Use t.Run subtests, t.Helper(), t.Cleanup()
- Do NOT use t.Parallel() (env var conflicts)
- Follow patterns from gcp-eventarc-emulator/integration_test.go



## Interface Contracts

### setupEmulator

Creates an in-process gRPC server with all four emulator services registered.
Used by all test files across both waves.


```
func setupEmulator(t *testing.T) (lis *bufconn.Listener, grpcAddr string, cleanup func())

Implementation details:
- Creates bufconn.Listen(1024 * 1024) for in-process gRPC testing
- Creates grpc.NewServer()
- Sets os.Setenv("IAM_MODE", "off") to disable IAM enforcement
- Calls iam.Register(grpcSrv) with no options
- Calls sm.Register(grpcSrv)
- Calls kms.Register(grpcSrv)
- Calls eventarc.Register(grpcSrv)
- Starts grpcSrv.Serve(lis) in a goroutine
- ALSO starts a real TCP listener on "localhost:0" and serves the same
  grpcSrv on it (needed for REST gateway which dials a real TCP address).
  Use net.Listen("tcp", "localhost:0") to get a free port, then serve in goroutine.
- Returns: lis (for bufconn gRPC tests), real TCP addr string (for gateway),
  and cleanup func that calls grpcSrv.Stop() and closes both listeners

```

### dialEmulator

Creates a gRPC client connection over the bufconn listener.


```
func dialEmulator(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn

Implementation:
- grpc.NewClient("passthrough://bufnet", grpc.WithContextDialer(...), insecure)
- Registers t.Cleanup(conn.Close)
- Returns conn

```

### startGateway

Starts the unified REST gateway against the real TCP gRPC address.


```
func startGateway(t *testing.T, grpcAddr string) string

Implementation:
- Calls gateway.New(grpcAddr)
- Creates httptest.NewServer(gw) wrapping the Gateway as http.Handler
- Registers t.Cleanup(ts.Close)
- Returns ts.URL (the gateway base URL for HTTP requests)

```



## Quality Gates

Level: standard

- **build**: `go build ./...` (required: true)
- **format**: `gofmt -l .` (required: false)
- **lint**: `go vet ./...` (required: true)
- **test**: `go test -count=1 ./...` (required: true)

