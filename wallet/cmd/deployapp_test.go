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
	velatestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/magiconair/properties"
	"github.com/stretchr/testify/require"
)

type deployAppTestBlockchainClient struct {
	pending       []*common.Request
	deployPayload []byte
	nextID        byte
}

func (c *deployAppTestBlockchainClient) SubmitRequest(_ context.Context, protocolVersion uint8, applicationId common.ApplicationIdType, requestType common.RequestType, payload []byte, tokenAddress ethCommon.Address, assetAmount *big.Int, maxFeeValue *big.Int) (common.RequestIdType, uint64, error) {
	c.nextID++
	var requestID common.RequestIdType
	requestID[31] = c.nextID

	c.pending = append(c.pending, &common.Request{
		ProtocolVersion: protocolVersion,
		ApplicationID:   applicationId,
		RequestID:       requestID,
		RequestType:     requestType,
		Payload:         payload,
		TokenAddress:    tokenAddress,
		AssetAmount:     common.ToBig(assetAmount),
		MaxFeeValue:     common.ToBig(maxFeeValue),
	})

	return requestID, 0, nil
}

func (c *deployAppTestBlockchainClient) SubmitDeployRequest(_ context.Context, protocolVersion uint8, payload []byte, maxFeeValue *big.Int) (common.ApplicationIdType, common.RequestIdType, uint64, error) {
	c.nextID++
	var requestID common.RequestIdType
	requestID[31] = c.nextID
	c.deployPayload = payload
	// Simulate contract assigning applicationId = 42
	return common.NewApplicationId(42), requestID, 0, nil
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

func (*deployAppTestBlockchainClient) GetPendingClaims(context.Context, ethCommon.Address, ethCommon.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (*deployAppTestBlockchainClient) Claim(context.Context, ethCommon.Address, ethCommon.Address) error {
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
	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})

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
		BlockchainPollingTimeout:  5,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.Exec(context.Background())
	require.NoError(t, err)

	require.NotEmpty(t, mockBC.deployPayload)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(mockBC.deployPayload, &payload))
	require.Equal(t, "artifact_ref", payload["mode"])
	require.Equal(t, "sha256:"+shaHex, payload["artifactId"])
	require.Equal(t, shaHex, payload["wasmSha256"])

	// Verify ApplicationID was persisted to wallet.conf and can be read back
	savedCfg, err := app.LoadConfigFromFile(confFile)
	require.NoError(t, err)
	require.Equal(t, common.NewApplicationId(42), savedCfg.ApplicationID)
}

func TestDeployAppCommand_FailsWithoutArtifactServiceURL(t *testing.T) {
	wasmPath := writeTempWASM(t, []byte("dummy-wasm-module"))
	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})

	cmd := NewDeployAppCommand(&app.Config{}, blockchain.NewMockClient(), confFile)
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.Exec(context.Background())
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

	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})
	mockBC := &deployAppTestBlockchainClient{}
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.Exec(context.Background())
	require.ErrorContains(t, err, "deploy upload hash mismatch")

	require.Empty(t, mockBC.deployPayload)
}

func TestDeployAppCommand_OverwritesExistingApplicationID(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHex := shaHex(wasmBytes)

	// Pre-existing wallet.conf with a different ApplicationID
	confFile := cmdtestutil.WriteTempConf(t, &app.Config{
		ApplicationID: common.NewApplicationId(7),
	})

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		BlockchainPollingTimeout:  5,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.Exec(context.Background())
	require.NoError(t, err)

	// Read the raw file — old value should be commented out
	data, err := os.ReadFile(confFile)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "# ApplicationID=7")
	require.Contains(t, content, "ApplicationID=42")

	// Round-trip: LoadConfigFromFile should read the new value
	savedCfg, err := app.LoadConfigFromFile(confFile)
	require.NoError(t, err)
	require.Equal(t, common.NewApplicationId(42), savedCfg.ApplicationID)
}

// TestDeployThenRestart_LoadedConfigUsesAssignedApplicationID verifies the full
// persistence round-trip across a simulated wallet restart:
//
//  1. A wallet.conf is created with an empty ApplicationID.
//  2. The deployapp command runs, the contract assigns ApplicationID=42,
//     and SaveApplicationID writes it to wallet.conf.
//  3. LoadConfigFromFile re-reads wallet.conf from disk (simulating a fresh
//     wallet process starting up after the deploy).
//  4. A deposit command is executed using the reloaded config. The test asserts
//     that SubmitRequest receives ApplicationID=42 — proving the value survived
//     the write-to-disk / read-from-disk cycle and is usable by subsequent commands.
func TestDeployThenRestart_LoadedConfigUsesAssignedApplicationID(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHex := shaHex(wasmBytes)

	// Phase 1: Create wallet.conf with empty ApplicationID and deploy
	confFile := cmdtestutil.WriteTempConf(t, &app.Config{
		AuthorityServiceURL:       "http://placeholder",
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
	})

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHex,
			"wasmSha256": shaHex,
		})
	}))
	defer artifactServer.Close()

	mockBC := &deployAppTestBlockchainClient{}
	deployCfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
	}

	deployCmd := NewDeployAppCommand(deployCfg, mockBC, confFile)
	deployCmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	deployCmd.wasmPath = wasmPath
	deployCmd.maxFeeValue = "100 wei"

	err := deployCmd.Exec(context.Background())
	require.NoError(t, err)

	// Phase 2: Simulate wallet restart — load config fresh from the file
	reloadedCfg, err := app.LoadConfigFromFile(confFile)
	require.NoError(t, err)
	require.Equal(t, common.NewApplicationId(42), reloadedCfg.ApplicationID,
		"reloaded config should have the ApplicationID written by deploy")

	// Phase 3: Use the reloaded config for a deposit command — verify it passes
	// the correct ApplicationID to SubmitRequest
	depositCmd := NewDepositCommand(reloadedCfg, mockBC)
	depositCmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd := depositCmd.Command()
	cmd.Flags().Set("amount", "1 wei")
	cmd.Flags().Set("max-value-fee", "1 wei")
	cmd.Run(cmd, nil)

	// The mock captured the SubmitRequest call — verify it used ApplicationID 42
	require.Len(t, mockBC.pending, 1, "deposit should have submitted one request")
	require.Equal(t, common.NewApplicationId(42), mockBC.pending[0].ApplicationID,
		"deposit should use the ApplicationID that was persisted by deploy")
}

// TestDeployAppCommand_UnauthorizedDeployerReverts verifies that the smart contract
// rejects deploy requests from accounts that lack the DEPLOYER_ROLE.
//
// This is an integration test against a simulated blockchain (not a mock). It exercises
// the real ProcessorEndpoint contract's access control. The DEPLOYER_ROLE is granted to
// testHelper.Deployer during contract deployment; the Submitter account is a regular user
// without that role.
//
// The rejection happens at the contract level (before the manager/executor are involved),
// so this test does not need the full system test infrastructure — just a blockchain client
// configured with a non-deployer signing key.
func TestDeployAppCommand_UnauthorizedDeployerReverts(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHex := shaHex(wasmBytes)
	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHex,
			"wasmSha256": shaHex,
		})
	}))
	defer artifactServer.Close()

	testHelper := velatestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	// Create a blockchain client using the Submitter account, which does NOT have
	// the DEPLOYER_ROLE. Only testHelper.Deployer has that role (granted during
	// contract construction).
	unauthorizedClient := blockchain.SetupNewBlockChainClientConnected(
		testHelper.Client(),
		testHelper.ProcessorContractAddress,
		testHelper.TeeSignerAddress,
		testHelper.Submitter,
	)

	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
	}

	cmd := NewDeployAppCommand(cfg, unauthorizedClient, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"

	err := cmd.Exec(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "DeployerNotAllowed",
		"contract should reject deploy from an account without DEPLOYER_ROLE")
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

// TestDeployAppCommand_WithAllowedTokens verifies that --allowed-tokens values
// are resolved via the wallet.conf token registry, normalized to lowercase hex,
// and embedded in the DeployDescriptor's ConstructorParams.
func TestDeployAppCommand_WithAllowedTokens(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHexStr := shaHex(wasmBytes)

	mockAddr := "0xCafeBabe00000000000000000000000000000001"
	confText := "token.MOCK.address=" + mockAddr + "\n" +
		"token.MOCK.decimals=18\n"
	props, err := properties.LoadString(confText)
	require.NoError(t, err)
	tokens, err := app.LoadTokenRegistry(props)
	require.NoError(t, err)

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHexStr,
			"wasmSha256": shaHexStr,
		})
	}))
	defer artifactServer.Close()

	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})
	mockBC := &deployAppTestBlockchainClient{}
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
		Tokens:                    tokens,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"
	// Mix of symbol, raw address (same token, should be deduped), ETH (should be
	// silently dropped since guest always allows it).
	cmd.allowedTokens = []string{"MOCK", mockAddr, "ETH"}

	require.NoError(t, cmd.Exec(context.Background()))

	// Decode the submitted deploy payload and inspect ConstructorParams
	var payload struct {
		Mode              string          `json:"mode"`
		ArtifactID        string          `json:"artifactId"`
		WasmSHA256        string          `json:"wasmSha256"`
		ConstructorParams json.RawMessage `json:"constructorParams"`
	}
	require.NoError(t, json.Unmarshal(mockBC.deployPayload, &payload))
	require.NotEmpty(t, payload.ConstructorParams, "ConstructorParams should be present")

	var ctor struct {
		AllowedTokens []string `json:"allowedTokens"`
	}
	require.NoError(t, json.Unmarshal(payload.ConstructorParams, &ctor))
	require.Len(t, ctor.AllowedTokens, 1, "MOCK+address dup should produce 1 entry; ETH should be dropped")
	require.Equal(t, strings.ToLower(mockAddr), ctor.AllowedTokens[0],
		"addresses must be lowercase hex to match the guest runtime's lookup format")
}

// TestDeployAppCommand_WithAllowedTokens_HexAddressOnly verifies that a raw
// checksummed hex address (no symbol) is resolved via the registry and the
// lowercase normalization happens even when the registry entry itself stores
// the address in checksummed form.
func TestDeployAppCommand_WithAllowedTokens_HexAddressOnly(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHexStr := shaHex(wasmBytes)

	// Register the token in the wallet under a checksummed address (mixed case).
	// The registry normalizes internally; the CLI must also normalize its output.
	mockAddrChecksum := "0xCafeBabe00000000000000000000000000000001"
	confText := "token.MOCK.address=" + mockAddrChecksum + "\n" +
		"token.MOCK.decimals=18\n"
	props, err := properties.LoadString(confText)
	require.NoError(t, err)
	tokens, err := app.LoadTokenRegistry(props)
	require.NoError(t, err)

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHexStr,
			"wasmSha256": shaHexStr,
		})
	}))
	defer artifactServer.Close()

	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})
	mockBC := &deployAppTestBlockchainClient{}
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
		Tokens:                    tokens,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"
	// Pass ONLY the raw hex address (no symbol)
	cmd.allowedTokens = []string{mockAddrChecksum}

	require.NoError(t, cmd.Exec(context.Background()))

	var payload struct {
		ConstructorParams json.RawMessage `json:"constructorParams"`
	}
	require.NoError(t, json.Unmarshal(mockBC.deployPayload, &payload))
	require.NotEmpty(t, payload.ConstructorParams)

	var ctor struct {
		AllowedTokens []string `json:"allowedTokens"`
	}
	require.NoError(t, json.Unmarshal(payload.ConstructorParams, &ctor))
	require.Equal(t, []string{strings.ToLower(mockAddrChecksum)}, ctor.AllowedTokens,
		"hex address must be normalized to lowercase in ConstructorParams")
}

// TestDeployAppCommand_WithAllowedTokens_MultipleDistinctTokens verifies that
// two distinct tokens are both included and ordering is preserved.
func TestDeployAppCommand_WithAllowedTokens_MultipleDistinctTokens(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHexStr := shaHex(wasmBytes)

	mockAddr := "0xCafeBabe00000000000000000000000000000001"
	usdcAddr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	confText := "token.MOCK.address=" + mockAddr + "\n" +
		"token.MOCK.decimals=18\n" +
		"token.USDC.address=" + usdcAddr + "\n" +
		"token.USDC.decimals=6\n"
	props, err := properties.LoadString(confText)
	require.NoError(t, err)
	tokens, err := app.LoadTokenRegistry(props)
	require.NoError(t, err)

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHexStr,
			"wasmSha256": shaHexStr,
		})
	}))
	defer artifactServer.Close()

	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})
	mockBC := &deployAppTestBlockchainClient{}
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
		Tokens:                    tokens,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"
	cmd.allowedTokens = []string{"MOCK", "USDC"}

	require.NoError(t, cmd.Exec(context.Background()))

	var payload struct {
		ConstructorParams json.RawMessage `json:"constructorParams"`
	}
	require.NoError(t, json.Unmarshal(mockBC.deployPayload, &payload))

	var ctor struct {
		AllowedTokens []string `json:"allowedTokens"`
	}
	require.NoError(t, json.Unmarshal(payload.ConstructorParams, &ctor))
	require.Equal(t,
		[]string{strings.ToLower(mockAddr), strings.ToLower(usdcAddr)},
		ctor.AllowedTokens,
		"both distinct tokens must appear in ConstructorParams in input order")
}

// TestDeployAppCommand_NoAllowedTokens_OmitsConstructorParams verifies that
// when --allowed-tokens is not set, the ConstructorParams field is absent
// from the marshaled DeployDescriptor (not an empty string, not "null").
// This prevents regression where we'd always serialize an empty field.
func TestDeployAppCommand_NoAllowedTokens_OmitsConstructorParams(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHexStr := shaHex(wasmBytes)

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHexStr,
			"wasmSha256": shaHexStr,
		})
	}))
	defer artifactServer.Close()

	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})
	mockBC := &deployAppTestBlockchainClient{}
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"
	// allowedTokens left as zero value (nil) — simulating no flag given

	require.NoError(t, cmd.Exec(context.Background()))

	// Decode into a map so we can check for field absence (not presence-with-zero-value)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(mockBC.deployPayload, &payload))
	_, present := payload["constructorParams"]
	require.False(t, present,
		"constructorParams must be absent from the descriptor when --allowed-tokens is not set; "+
			"found: %v", payload["constructorParams"])
}

// TestDeployAppCommand_WithAllowedTokens_UnknownSymbol verifies that an
// unresolved token symbol produces a clear error and does not submit a request.
func TestDeployAppCommand_WithAllowedTokens_UnknownSymbol(t *testing.T) {
	wasmBytes := []byte("dummy-wasm-module")
	wasmPath := writeTempWASM(t, wasmBytes)
	shaHexStr := shaHex(wasmBytes)

	tokens, err := app.LoadTokenRegistry(nil) // registry with only ETH
	require.NoError(t, err)

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifactId": "sha256:" + shaHexStr,
			"wasmSha256": shaHexStr,
		})
	}))
	defer artifactServer.Close()

	confFile := cmdtestutil.WriteTempConf(t, &app.Config{})
	mockBC := &deployAppTestBlockchainClient{}
	cfg := &app.Config{
		AuthorityServiceURL:       artifactServer.URL,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  5,
		Tokens:                    tokens,
	}

	cmd := NewDeployAppCommand(cfg, mockBC, confFile)
	cmd.SubgraphClient = cmdtestutil.SubgraphClientOK()
	cmd.wasmPath = wasmPath
	cmd.maxFeeValue = "100 wei"
	cmd.allowedTokens = []string{"UNKNOWN"}

	err = cmd.Exec(context.Background())
	require.ErrorContains(t, err, "--allowed-tokens")
	require.Empty(t, mockBC.deployPayload, "no request should be submitted when resolution fails")
}
