package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	pestestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDeployAppCommand_Success(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHex := shaHex(wasmBytes)

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/deploy/upload", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(32<<20))

		file, _, err := r.FormFile("wasm")
		require.NoError(t, err)
		defer file.Close()
		uploaded, err := io.ReadAll(file)
		require.NoError(t, err)
		require.Equal(t, wasmBytes, uploaded)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHex,
			"wasmSha256": shaHex,
		})
	}))
	defer artifactServer.Close()

	mockBC := blockchain.NewMockClient()
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  1,
	}

	cmd := NewDeployAppCommand(cfg, mockBC)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.run(context.Background())
	require.NoError(t, err)

	pending, err := mockBC.GetPendingRequests(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 1)

	req := pending[0]
	require.Equal(t, common.Deploy, req.RequestType)
	require.NotEmpty(t, req.Payload)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(req.Payload, &payload))
	require.Equal(t, "artifact_ref", payload["mode"])
	require.Equal(t, "sha256:"+shaHex, payload["artifactId"])
	require.Equal(t, shaHex, payload["wasmSha256"])
}

func TestDeployAppCommand_FailsWithoutArtifactServiceURL(t *testing.T) {
	wasmPath := writeTempWASM(t, []byte("dummy-wasm-module"))

	cmd := NewDeployAppCommand(&app.Config{}, blockchain.NewMockClient())
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.run(context.Background())
	require.ErrorContains(t, err, "authority service URL is required")
}

func TestDeployAppCommand_FailsOnUploadHashMismatch(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + strings.Repeat("0", 64),
			"wasmSha256": strings.Repeat("0", 64),
		})
	}))
	defer artifactServer.Close()

	mockBC := blockchain.NewMockClient()
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  1,
	}

	cmd := NewDeployAppCommand(cfg, mockBC)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.run(context.Background())
	require.ErrorContains(t, err, "deploy upload hash mismatch")

	pending, getErr := mockBC.GetPendingRequests(context.Background())
	require.NoError(t, getErr)
	require.Len(t, pending, 0)
}

func writeTempWASM(t *testing.T, wasm []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.wasm")
	require.NoError(t, os.WriteFile(path, wasm, 0o644))
	return path
}

func shaHex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
