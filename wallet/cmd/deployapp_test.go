package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	cmdtestutil "github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type deployAppTestBlockchainClient struct {
	pending []*common.Request
	nextID  byte
}

func (c *deployAppTestBlockchainClient) SubmitRequest(_ context.Context, protocolVersion uint8, applicationId common.ApplicationIdType, requestType common.RequestType, payload []byte, depositAmount *big.Int, maxFeeValue *big.Int) (common.RequestIdType, uint64, error) {
	c.nextID++
	var requestID common.RequestIdType
	requestID[31] = c.nextID

	c.pending = append(c.pending, &common.Request{
		ProtocolVersion: protocolVersion,
		ApplicationID:   applicationId,
		RequestID:       requestID,
		RequestType:     requestType,
		Payload:         payload,
		DepositAmount:   common.ToBig(depositAmount),
		MaxFeeValue:     common.ToBig(maxFeeValue),
	})

	return requestID, 0, nil
}

func (c *deployAppTestBlockchainClient) GetPendingRequests(_ context.Context) ([]*common.Request, error) {
	return append([]*common.Request(nil), c.pending...), nil
}

func (*deployAppTestBlockchainClient) GetNextPendingRequest(context.Context) (*common.Request, [32]byte, error) {
	return nil, [32]byte{}, nil
}

func (*deployAppTestBlockchainClient) SubmitStateUpdate(context.Context, *common.UpdatePayload) error {
	return nil
}

func (*deployAppTestBlockchainClient) GetTeePublicKey(context.Context) (*cryptotypes.PublicKeyP521, error) {
	return nil, nil
}

func (*deployAppTestBlockchainClient) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (*deployAppTestBlockchainClient) LatestBlockNumber(context.Context) (uint64, error) {
	return 0, nil
}

func (*deployAppTestBlockchainClient) GetPendingPayments(context.Context, ethCommon.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (*deployAppTestBlockchainClient) WithdrawPayments(context.Context, ethCommon.Address) error {
	return nil
}

func (*deployAppTestBlockchainClient) Close() error {
	return nil
}

func (*deployAppTestBlockchainClient) Connect(context.Context) error {
	return nil
}

func (*deployAppTestBlockchainClient) IsConnected() bool {
	return true
}

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

	mockBC := &deployAppTestBlockchainClient{}
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

	mockBC := &deployAppTestBlockchainClient{}
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
