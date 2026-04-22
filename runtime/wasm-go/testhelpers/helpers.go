// Package testhelpers provides helpers shared between vela-nova's system_tests
// (mock suite) and fullstack_tests (real-chain suite). Before this package
// existed, each test folder owned local copies of these helpers; they drifted
// over time and fixes in one folder didn't reach the other.
//
// Helpers that need to drive the suite accept a DeploySuite — a minimal
// interface satisfied by both *testutil.SystemTestSuite and
// *fullstack.FullStackSystemTestSuite. Helpers that do not need the suite
// (WASM build, log config) are plain functions.
package testhelpers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/HorizenOfficial/vela/pkg/authorityservice/deployartifact"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	commontestutil "github.com/HorizenOfficial/vela/pkg/common/testutil"
	"github.com/HorizenOfficial/vela/pkg/logger"
	"github.com/HorizenOfficial/vela/pkg/testutil"
	"github.com/stretchr/testify/require"
	ethCommon "github.com/ethereum/go-ethereum/common"
)

// DeploySuite is the minimal behavior the shared helpers need from a test
// suite. Both *testutil.SystemTestSuite (mock) and
// *fullstack.FullStackSystemTestSuite satisfy it via methods on their embedded
// *testutil.TestSuiteCore plus their own SubmitRequest/AssertRequestCompleted
// implementations.
//
// A narrower split (one interface per helper) was considered and rejected:
// both suites implement all three methods, and fragmenting adds noise without
// real decoupling benefit. If a future helper genuinely needs only a subset,
// it can declare its own inline interface — Go satisfies them implicitly.
type DeploySuite interface {
	GetArtifactsPath() string
	SubmitRequest(*common.Request) error
	AssertRequestCompleted(common.RequestIdType, time.Duration) error
}

// BuildAndLoadWasmModule builds the payment-app WASM via `make build` and
// returns the compiled bytecode. Invokes TinyGo; expects the runtime/wasm-go
// directory to be reachable via ../.. from this package file.
func BuildAndLoadWasmModule(t *testing.T) []byte {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	// This file lives at runtime/wasm-go/testhelpers/helpers.go — the WASM
	// source is one directory up (runtime/wasm-go).
	appDir := filepath.Join(filepath.Dir(thisFile), "..")

	cmd := exec.Command("make", "build")
	cmd.Dir = appDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build wasm module: %s", string(output))

	wasmPath := filepath.Join(appDir, "build", "payment_app.wasm")
	wasmBytecode, err := os.ReadFile(wasmPath)
	require.NoError(t, err)
	require.NotEmpty(t, wasmBytecode)

	return wasmBytecode
}

// UploadArtifactAndBuildDescriptorPayload writes the WASM bytes to the suite's
// deploy-artifact store and returns a deploy descriptor (JSON) that references
// the stored artifact by SHA-256. Matches the shape the manager expects on
// deploy request processing.
func UploadArtifactAndBuildDescriptorPayload(t *testing.T, suite DeploySuite, wasmBytecode []byte) []byte {
	t.Helper()

	store, err := deployartifact.NewStore(suite.GetArtifactsPath())
	require.NoError(t, err)
	uploadAPI := deployartifact.NewAPI(store, 50, logger.NewLogger(&logger.Config{Kind: "zerolog", Console: false}))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("wasm", "app.wasm")
	require.NoError(t, err)
	_, err = fileWriter.Write(wasmBytecode)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/deploy/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	uploadAPI.HandleUpload(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var uploadResp deployartifact.UploadResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &uploadResp))

	localSum := sha256.Sum256(wasmBytecode)
	localSHA := hex.EncodeToString(localSum[:])
	localArtifactID, err := common.BuildArtifactID(localSHA)
	require.NoError(t, err)
	require.Equal(t, localSHA, uploadResp.WasmSHA256)
	require.Equal(t, localArtifactID, uploadResp.ArtifactID)

	descriptor := common.DeployDescriptor{
		Mode:       common.DeployModeArtifactRef,
		ArtifactID: uploadResp.ArtifactID,
		WasmSHA256: uploadResp.WasmSHA256,
	}
	payload, err := json.Marshal(descriptor)
	require.NoError(t, err)
	return payload
}

// RegisterSeedUser generates a user P521 key via the CryptoHelper, submits an
// AssociateKey request that carries the encrypted seed, and waits for
// completion. After registration the user's events can be retrieved via
// WaitForEventBySubtypes with the hashed-subtype scheme.
//
// Uses req.RequestID after SubmitRequest so the helper works against both
// suite variants: the mock suite leaves req.RequestID untouched; the
// fullstack suite overwrites it with the contract-assigned ID.
func RegisterSeedUser(
	t *testing.T,
	suite DeploySuite,
	cryptoHelper *testutil.CryptoHelper,
	executorPubKey *cryptotypes.PublicKeyP521,
	appID common.ApplicationIdType,
	user ethCommon.Address,
	timeout time.Duration,
) {
	t.Helper()
	userKey, err := cryptoHelper.GenerateUserKey(user)
	require.NoError(t, err)
	reqID := commontestutil.GenerateRandomRequestID()
	req, err := cryptoHelper.CreateAssociateKeyRequest(appID, reqID, user, userKey.PublicKey(), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(req))
	require.NoError(t, suite.AssertRequestCompleted(req.RequestID, timeout))
}

// NewNetworkLogConfig returns a logger.Config configured for the zeronetwork
// kind (logs forwarded to the in-test log server). The suite injects the
// correct log-server port into RemoteLogParams before creating the logger, so
// no hardcoded port is needed here.
func NewNetworkLogConfig() *logger.Config {
	return &logger.Config{
		Kind:             "zeronetwork",
		ConsoleColor:     false,
		Console:          true,
		ConsoleLevel:     "trace",
		FileLevel:        "trace",
		RemoteLogNetwork: "tcp",
		NetworkLevel:     "trace",
	}
}
