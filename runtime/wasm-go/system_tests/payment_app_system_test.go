package main_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HorizenOfficial/vela/pkg/authorityservice/deployartifact"
	"github.com/HorizenOfficial/vela/pkg/common"
	commontestutil "github.com/HorizenOfficial/vela/pkg/common/testutil"
	"github.com/HorizenOfficial/vela/pkg/executor"
	"github.com/HorizenOfficial/vela/pkg/logger"
	systemTests "github.com/HorizenOfficial/vela/pkg/testutil"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// host-side event types for test validation (app-specific, not framework types).
// The guest always emits TokenAddress on deposit/withdrawal events — 0x0 for ETH,
// non-zero for ERC-20. Host types include the field so ERC-20 tests can assert it;
// ETH tests see a zero-value Address, which is the correct semantic.
type hostDepositEvent struct {
	Type         string            `json:"type"`
	TokenAddress ethCommon.Address `json:"tokenAddress"`
	Amount       *common.Big       `json:"amount"`
	Balance      *common.Big       `json:"balance"`
	Nonce        uint64            `json:"nonce"`
}

type hostWithdrawalEvent struct {
	Type         string            `json:"type"`
	To           ethCommon.Address `json:"to"`
	TokenAddress ethCommon.Address `json:"tokenAddress"`
	Amount       *common.Big       `json:"amount"`
	Balance      *common.Big       `json:"balance"`
	Nonce        uint64            `json:"nonce"`
}

// depositToPaymentApp is a helper function to deposit funds and validate the deposit event.
// tokenAddress = 0x0 for ETH, non-zero for ERC-20.
// When useSeed is true the event is matched via the hashed subtype set (seed-registered user).
// When useSeed is false the event is matched via the plaintext "deposit" subtype (no-seed user).
func depositToPaymentApp(t *testing.T, suite *systemTests.SystemTestSuite, cryptoHelper *systemTests.CryptoHelper, appID common.ApplicationIdType, reqID common.RequestIdType, user ethCommon.Address, tokenAddress ethCommon.Address, amount *big.Int, useSeed bool) {
	t.Helper()
	timeout := 100 * time.Second

	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	depositReq, err := cryptoHelper.CreateTokenDepositRequest(appID, reqID, user, tokenAddress, amount, executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(depositReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	var depositEvent *common.Event
	if useSeed {
		userSeed, err := cryptoHelper.ComputeSeed(user)
		require.NoError(t, err)
		depositEvent, err = suite.WaitForEventBySubtypes(user, executor.AllSubtypes(userSeed, executor.DefaultSubtypeN), timeout)
		require.NoError(t, err)
	} else {
		depositEvent, err = suite.WaitForEvent(user, "deposit", timeout)
		require.NoError(t, err)
	}

	decryptedData, err := cryptoHelper.DecryptEvent(user, depositEvent, executorPubKey)
	require.NoError(t, err)

	var eventData hostDepositEvent
	err = json.Unmarshal(decryptedData, &eventData)
	require.NoError(t, err)
	require.Equal(t, "deposit", eventData.Type)
	require.Equal(t, tokenAddress, eventData.TokenAddress,
		"deposit event must carry the correct tokenAddress (0x0 for ETH)")
	require.Equal(t, 0, amount.Cmp(eventData.Amount.ToInt()))

	executorSigningKey, err := suite.GetExecutorSigningKey()
	require.NoError(t, err)
	payload, err := suite.GetRequestUpdatePayload(reqID)
	require.NoError(t, err)
	err = cryptoHelper.ValidateUpdatePayloadSignature(payload, executorSigningKey)
	require.NoError(t, err)
}

// withdrawFromPaymentApp is a helper function to withdraw funds and validate the withdrawal event.
// tokenAddress = 0x0 for ETH, non-zero for ERC-20.
func withdrawFromPaymentApp(t *testing.T, suite *systemTests.SystemTestSuite, cryptoHelper *systemTests.CryptoHelper, appID common.ApplicationIdType, reqID common.RequestIdType, user, recipient ethCommon.Address, tokenAddress ethCommon.Address, amount *big.Int) {
	t.Helper()
	timeout := 100 * time.Second

	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	withdrawalReq, err := cryptoHelper.CreateTokenWithdrawalRequest(appID, reqID, user, recipient, tokenAddress, common.ToBig(amount), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(withdrawalReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	userSeed, err := cryptoHelper.ComputeSeed(user)
	require.NoError(t, err)
	withdrawalEvent, err := suite.WaitForEventBySubtypes(user, executor.AllSubtypes(userSeed, executor.DefaultSubtypeN), timeout)
	require.NoError(t, err)
	decryptedData, err := cryptoHelper.DecryptEvent(user, withdrawalEvent, executorPubKey)
	require.NoError(t, err)

	var eventData hostWithdrawalEvent
	err = json.Unmarshal(decryptedData, &eventData)
	require.NoError(t, err)
	require.Equal(t, "withdrawal", eventData.Type)
	require.Equal(t, recipient, eventData.To)
	require.Equal(t, tokenAddress, eventData.TokenAddress,
		"withdrawal event must carry the correct tokenAddress (0x0 for ETH)")
	require.Equal(t, 0, amount.Cmp(eventData.Amount.ToInt()))

	// Verify mock-chain Withdrawal record
	withdrawal, err := suite.WaitForWithdrawal(appID, timeout)
	require.NoError(t, err)
	require.NotNil(t, withdrawal)
	require.Equal(t, recipient, withdrawal.DestinationAddress)
	require.Equal(t, tokenAddress, withdrawal.TokenAddress,
		"mock-chain Withdrawal record must carry the correct tokenAddress (0x0 for ETH)")
	require.Equal(t, 0, amount.Cmp(withdrawal.Amount.ToInt()))

	executorSigningKey, err := suite.GetExecutorSigningKey()
	require.NoError(t, err)
	payload, err := suite.GetRequestUpdatePayload(reqID)
	require.NoError(t, err)
	err = cryptoHelper.ValidateUpdatePayloadSignature(payload, executorSigningKey)
	require.NoError(t, err)
}

// storeWasmArtifact writes wasmBytecode into the manager's artifact blob store
// (artifactsPath/blobs/<sha256>.wasm) and returns the JSON DeployDescriptor payload
// that references it — required by the vela v0.0.26 deploy protocol.
func storeWasmArtifact(t *testing.T, artifactsPath string, wasmBytecode []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(wasmBytecode)
	shaHex := hex.EncodeToString(sum[:])

	blobsDir := filepath.Join(artifactsPath, "blobs")
	require.NoError(t, os.MkdirAll(blobsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blobsDir, shaHex+".wasm"), wasmBytecode, 0o644))

	payload, err := json.Marshal(map[string]string{
		"mode":       "artifact_ref",
		"artifactId": "sha256:" + shaHex,
		"wasmSha256": shaHex,
	})
	require.NoError(t, err)
	return payload
}

// buildAndLoadWasmModule is a helper function to build the wasm module and read its bytecode.
func buildAndLoadWasmModule(t *testing.T) []byte {
	// Get the project root directory to construct absolute paths
	_, b, _, ok := runtime.Caller(0)
	require.True(t, ok)
	//appDir := filepath.Join(filepath.Dir(b), "../..")
	projectRoot := filepath.Join(filepath.Dir(b), "../..")
	appDir := filepath.Join(projectRoot, "wasm-go")

	// Build the wasm module
	cmd := exec.Command("make", "build")
	cmd.Dir = appDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build wasm module: %s", string(output))

	// Load wasm bytecode for the wasm app
	wasmPath := filepath.Join(appDir, "build", "payment_app.wasm")
	wasmBytecode, err := os.ReadFile(wasmPath)
	require.NoError(t, err)
	require.NotEmpty(t, wasmBytecode)

	return wasmBytecode
}

// uploadArtifactAndBuildDescriptorPayload uploads a WASM artifact to the local
// artifact store and returns a deploy descriptor payload (JSON) that references it.
func uploadArtifactAndBuildDescriptorPayload(t *testing.T, suite *systemTests.SystemTestSuite, wasmBytecode []byte) []byte {
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

// buildDeployDescriptorWithTokens uploads a WASM artifact and returns a deploy
// descriptor payload (JSON) whose ConstructorParams.allowedTokens lists the
// given token addresses (lowercase hex). Matches the shape the wallet's
// `deployapp --allowed-tokens` CLI produces.
//
// Passing an empty slice is equivalent to uploadArtifactAndBuildDescriptorPayload
// (no ConstructorParams field in the output).
func buildDeployDescriptorWithTokens(t *testing.T, suite *systemTests.SystemTestSuite, wasmBytecode []byte, allowedTokens []ethCommon.Address) []byte {
	t.Helper()

	// Reuse the existing uploader, then layer ConstructorParams on top.
	basePayload := uploadArtifactAndBuildDescriptorPayload(t, suite, wasmBytecode)
	if len(allowedTokens) == 0 {
		return basePayload
	}

	var descriptor common.DeployDescriptor
	require.NoError(t, json.Unmarshal(basePayload, &descriptor))

	resolved := make([]string, 0, len(allowedTokens))
	for _, addr := range allowedTokens {
		resolved = append(resolved, strings.ToLower(addr.Hex()))
	}
	params := struct {
		AllowedTokens []string `json:"allowedTokens"`
	}{AllowedTokens: resolved}
	ctorBytes, err := json.Marshal(params)
	require.NoError(t, err)
	descriptor.ConstructorParams = ctorBytes

	payload, err := json.Marshal(descriptor)
	require.NoError(t, err)
	return payload
}

func TestPaymentAppFullFlow(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	// manager.LoadConfig() requires MANAGER_ARTIFACTS_PATH to be set.
	// NewSystemTestSuiteWithConfigs will override this with its own temp dir, but LoadConfig
	// must pass validation first.
	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	// The suite accepts logger configs (not instances) so it can inject the
	// ephemeral log-server port into RemoteLogParams before creating the loggers.
	// This guarantees the zeronetwork logger connects to the correct address.
	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment", newNetworkLogConfig(), newNetworkLogConfig())
	defer suite.Cleanup()

	wasmBytecode := buildAndLoadWasmModule(t)

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	appID := common.NewApplicationId(1)
	timeout := 100 * time.Second

	// vela v0.0.26: CreateAssociateKeyRequest requires a secp256k1 signing key whose
	// derived Ethereum address matches the sender. Use GenerateUserIdentity so the
	// address is derived from the key (rather than hardcoded hex addresses).
	cryptoHelper := systemTests.NewCryptoHelper()
	userAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)
	auditorAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)
	recipientAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")

	// Deploy the application using deploy descriptor (upload artifact first)
	deployPayload := uploadArtifactAndBuildDescriptorPayload(t, suite, wasmBytecode)
	deployReq := &common.Request{
		RequestType:   common.Deploy,
		ApplicationID: appID,
		RequestID:     commontestutil.GenerateRandomRequestID(),
		Payload:       deployPayload,
		Sender:        userAddress,
		Timestamp:     common.ToBig(new(big.Int).SetInt64(time.Now().Unix())),
		AssetAmount:   common.NewBig(0),
		MaxFeeValue:   common.NewBig(100),
	}
	require.NoError(t, suite.SubmitRequest(deployReq))
	_, err = suite.WaitForAppStateInDB(appID, timeout)
	require.NoError(t, err)
	_, err = suite.WaitForAppStateInBlockchain(appID, timeout)
	require.NoError(t, err)

	// Get executor communication key for associate key requests
	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	// Register user key
	userKey, err := cryptoHelper.GenerateUserKey(userAddress)
	require.NoError(t, err)
	reqID := commontestutil.GenerateRandomRequestID()
	associateKeyReq, err := cryptoHelper.CreateAssociateKeyRequest(appID, reqID, userAddress, userKey.PublicKey(), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(associateKeyReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	// Register auditor key
	auditorKey, err := cryptoHelper.GenerateUserKey(auditorAddress)
	require.NoError(t, err)
	reqID = commontestutil.GenerateRandomRequestID()
	associateAuditorReq, err := cryptoHelper.CreateAssociateKeyRequest(appID, reqID, auditorAddress, auditorKey.PublicKey(), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(associateAuditorReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	// Deposit 2 ETH and validate event fields (seed-registered user -> hashed subtypes)
	depositAmount := big.NewInt(2000000000000000000)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, ethCommon.Address{}, depositAmount, true)

	// Withdraw 0.5 ETH and validate event fields
	withdrawAmount := big.NewInt(500000000000000000)
	withdrawFromPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, recipientAddress, ethCommon.Address{}, withdrawAmount)

	// --- No-seed user: register without a seed, deposit, and verify that
	// WaitForEvent with the plaintext "deposit" subtype works. When no seed
	// is registered the framework preserves the WASM-provided subtype as-is.
	noSeedUser, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)
	noSeedKey, err := cryptoHelper.GenerateUserKey(noSeedUser)
	require.NoError(t, err)

	// Build an AssociateKey request with only the P521 public key (133 bytes, no seed)
	reqID = commontestutil.GenerateRandomRequestID()
	noSeedAssocReq := &common.Request{
		ApplicationID: appID,
		RequestID:     reqID,
		RequestType:   common.AssociateKey,
		Payload:       noSeedKey.PublicKey().Bytes(), // 133 bytes, no encrypted seed
		Sender:        noSeedUser,
		Timestamp:     common.ToBig(new(big.Int).SetInt64(time.Now().Unix())),
		AssetAmount:   common.NewBig(0),
		TokenAddress:  ethCommon.Address{},
		MaxFeeValue:   common.NewBig(100),
	}
	require.NoError(t, suite.SubmitRequest(noSeedAssocReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	// Deposit 1 ETH for the no-seed user; plaintext "deposit" subtype should match
	noSeedDepositAmount := big.NewInt(1000000000000000000)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), noSeedUser, ethCommon.Address{}, noSeedDepositAmount, false)

	// Deanonymization report as auditor — verifies final state after deposit and withdrawal
	reqID = commontestutil.GenerateRandomRequestID()
	deanonReq, err := cryptoHelper.CreateDeanonymizationRequest(appID, reqID, auditorAddress, []byte("{}"), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(deanonReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	deanonReport, err := suite.WaitForDeanonymizationReport(reqID, timeout)
	require.NoError(t, err)
	require.NotNil(t, deanonReport)

	decryptedReport, err := cryptoHelper.DecryptDeanonymizationReport(auditorAddress, deanonReport, executorPubKey)
	require.NoError(t, err)

	var report struct {
		ApplicationId   common.ApplicationIdType `json:"applicationId"`
		RequestId       common.RequestIdType     `json:"requestId"`
		ReportDataBytes interface{}              `json:"reportDataBytes"`
	}
	err = json.Unmarshal(decryptedReport, &report)
	require.NoError(t, err)
	require.Equal(t, appID, report.ApplicationId)
	require.Equal(t, reqID, report.RequestId)

	jsonStr, ok := report.ReportDataBytes.(string)
	require.True(t, ok, "reportDataBytes is not a string")
	reportBytes, err := base64.StdEncoding.DecodeString(jsonStr)
	require.NoError(t, err, "reportDataBytes is not base64 encoded")

	var reportData map[string]interface{}
	err = json.Unmarshal(reportBytes, &reportData)
	require.NoError(t, err)
	require.Contains(t, reportData, "accounts")
	require.Contains(t, reportData, "nonce")

	// Verify balances: seed user (2 ETH - 0.5 ETH = 1.5 ETH) and no-seed user (1 ETH)
	accounts, ok := reportData["accounts"].(map[string]interface{})
	require.True(t, ok, "accounts is not a map")
	require.Len(t, accounts, 2, "expected two accounts in report (seed user + no-seed user)")

	ethTokenHex := ethCommon.Address{}.Hex()
	expectedBalances := map[ethCommon.Address]*big.Int{
		userAddress: new(big.Int).Sub(depositAmount, withdrawAmount), // 1.5 ETH
		noSeedUser:  noSeedDepositAmount,                             // 1 ETH
	}
	for addrHex, acct := range accounts {
		acctMap, ok := acct.(map[string]interface{})
		require.True(t, ok, "account entry is not a map")
		balances, ok := acctMap["balances"].(map[string]interface{})
		require.True(t, ok, "balances is not a map")
		balanceStr, ok := balances[ethTokenHex].(string)
		require.True(t, ok, "ETH balance is not a string for account %s", addrHex)
		require.True(t, len(balanceStr) > 2 && balanceStr[:2] == "0x", "balance is not hex")
		balance, ok := new(big.Int).SetString(balanceStr[2:], 16)
		require.True(t, ok, "failed to parse balance hex")

		addr := ethCommon.HexToAddress(addrHex)
		expected, known := expectedBalances[addr]
		require.True(t, known, "unexpected account %s in report", addrHex)
		require.Equal(t, 0, expected.Cmp(balance),
			"account %s: expected balance %s, got %s", addrHex, expected, balance)
	}
}

// ----------------------------------------------------------------------------
// ERC-20 tests
// ----------------------------------------------------------------------------
//
// Scope: these tests exercise the ERC-20 flow through the manager + executor +
// TinyGo WASM stack. SystemTestSuite uses an in-memory MockClient (not a
// simulated blockchain), so they cover:
//
//   - Deploy with ConstructorParams.AllowedTokens populated
//   - Requests carrying TokenAddress (deposit / withdrawal / transfer)
//   - Guest-side AllowedTokens validation
//   - Decrypted event fields (tokenAddress, amount, balance)
//   - Mock-chain Withdrawal records (tokenAddress field)
//
// They do NOT exercise on-chain mechanics: ProcessorEndpoint.submitRequest(),
// EIP-2612 permit verification, ERC-20 permit + transferFrom, appCustody /
// pendingClaims accounting, or claim() execution. Those belong to tests that
// run against a simulated Ethereum backend (SimTestHelper in the blockchain
// package).
//
// Synthetic ERC-20 addresses are used throughout because MockClient doesn't
// validate them against a deployed contract. This is intentional — we're
// testing token-identity plumbing through the WASM/executor/manager stack,
// not a real deployed token.

// TestPaymentAppERC20FullFlow is the ERC-20 counterpart to
// TestPaymentAppFullFlow: deploy with AllowedTokens → register user →
// ERC-20 deposit → ERC-20 withdrawal → deanonymization report asserts the
// remaining balance is keyed under the token address.
func TestPaymentAppERC20FullFlow(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment-erc20", newNetworkLogConfig(), newNetworkLogConfig())
	defer suite.Cleanup()

	wasmBytecode := buildAndLoadWasmModule(t)

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	appID := common.NewApplicationId(1)
	timeout := 100 * time.Second

	// Synthetic ERC-20 token address. Not a deployed contract — see scope
	// comment at top of this section.
	tokenAddress := ethCommon.HexToAddress("0x000000000000000000000000000000000000C0DE")
	recipientAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")

	cryptoHelper := systemTests.NewCryptoHelper()
	userAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)
	auditorAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)

	// --- Deploy with AllowedTokens = [tokenAddress] ---
	deployPayload := buildDeployDescriptorWithTokens(t, suite, wasmBytecode, []ethCommon.Address{tokenAddress})
	deployReq := &common.Request{
		RequestType:   common.Deploy,
		ApplicationID: appID,
		RequestID:     commontestutil.GenerateRandomRequestID(),
		Payload:       deployPayload,
		Sender:        userAddress,
		Timestamp:     common.ToBig(new(big.Int).SetInt64(time.Now().Unix())),
		AssetAmount:   common.NewBig(0),
		MaxFeeValue:   common.NewBig(100),
	}
	require.NoError(t, suite.SubmitRequest(deployReq))
	_, err = suite.WaitForAppStateInDB(appID, timeout)
	require.NoError(t, err)
	_, err = suite.WaitForAppStateInBlockchain(appID, timeout)
	require.NoError(t, err)

	// The stored ApplicationState.EncryptedState is encrypted by the executor;
	// we can't read AllowedTokens directly. Token allowlisting is validated
	// implicitly: if the guest didn't receive ConstructorParams.AllowedTokens
	// containing tokenAddress, the deposit below would be rejected by the
	// guest and AssertRequestCompleted would fail. The decrypted deanonymization
	// report at the end also shows the balance keyed under tokenAddress.
	ethHex := strings.ToLower(ethCommon.Address{}.Hex())
	tokenHex := strings.ToLower(tokenAddress.Hex())

	// --- Register user and auditor keys ---
	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	userKey, err := cryptoHelper.GenerateUserKey(userAddress)
	require.NoError(t, err)
	reqID := commontestutil.GenerateRandomRequestID()
	associateKeyReq, err := cryptoHelper.CreateAssociateKeyRequest(appID, reqID, userAddress, userKey.PublicKey(), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(associateKeyReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	auditorKey, err := cryptoHelper.GenerateUserKey(auditorAddress)
	require.NoError(t, err)
	reqID = commontestutil.GenerateRandomRequestID()
	associateAuditorReq, err := cryptoHelper.CreateAssociateKeyRequest(appID, reqID, auditorAddress, auditorKey.PublicKey(), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(associateAuditorReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	// --- Deposit the ERC-20 token ---
	depositAmount := big.NewInt(1_000_000) // arbitrary; token decimals are irrelevant here
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, tokenAddress, depositAmount, true)

	// --- Withdraw a portion ---
	withdrawAmount := big.NewInt(400_000)
	withdrawFromPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, recipientAddress, tokenAddress, withdrawAmount)

	// --- Deanonymization report: remaining balance keyed under the token ---
	reqID = commontestutil.GenerateRandomRequestID()
	deanonReq, err := cryptoHelper.CreateDeanonymizationRequest(appID, reqID, auditorAddress, []byte("{}"), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(deanonReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	deanonReport, err := suite.WaitForDeanonymizationReport(reqID, timeout)
	require.NoError(t, err)
	require.NotNil(t, deanonReport)

	decryptedReport, err := cryptoHelper.DecryptDeanonymizationReport(auditorAddress, deanonReport, executorPubKey)
	require.NoError(t, err)

	var report struct {
		ApplicationId   common.ApplicationIdType `json:"applicationId"`
		RequestId       common.RequestIdType     `json:"requestId"`
		ReportDataBytes interface{}              `json:"reportDataBytes"`
	}
	require.NoError(t, json.Unmarshal(decryptedReport, &report))
	require.Equal(t, appID, report.ApplicationId)
	require.Equal(t, reqID, report.RequestId)

	jsonStr, ok := report.ReportDataBytes.(string)
	require.True(t, ok, "reportDataBytes is not a string")
	reportBytes, err := base64.StdEncoding.DecodeString(jsonStr)
	require.NoError(t, err, "reportDataBytes is not base64 encoded")

	var reportData map[string]interface{}
	require.NoError(t, json.Unmarshal(reportBytes, &reportData))
	accounts, ok := reportData["accounts"].(map[string]interface{})
	require.True(t, ok, "accounts is not a map")
	require.Len(t, accounts, 1, "expected exactly one account in report")

	expectedRemaining := new(big.Int).Sub(depositAmount, withdrawAmount)
	for addrHex, acct := range accounts {
		acctMap, ok := acct.(map[string]interface{})
		require.True(t, ok, "account entry is not a map")
		balances, ok := acctMap["balances"].(map[string]interface{})
		require.True(t, ok, "balances is not a map")

		// Remaining balance must be keyed under the ERC-20 token, not ETH.
		balanceStr, ok := balances[tokenHex].(string)
		require.True(t, ok, "balance for token %s missing on account %s", tokenHex, addrHex)
		require.True(t, len(balanceStr) > 2 && balanceStr[:2] == "0x", "balance is not hex-prefixed")
		parsed, ok := new(big.Int).SetString(balanceStr[2:], 16)
		require.True(t, ok, "failed to parse balance hex")
		require.Equal(t, 0, expectedRemaining.Cmp(parsed),
			"account %s: expected token balance %s, got %s", addrHex, expectedRemaining, parsed)

		// No ETH deposit was made; any ETH entry must be zero.
		if ethBalanceStr, hasETH := balances[ethHex].(string); hasETH {
			ethParsed, _ := new(big.Int).SetString(ethBalanceStr[2:], 16)
			require.Equal(t, 0, ethParsed.Sign(),
				"account %s should have no ETH balance, got %s", addrHex, ethBalanceStr)
		}
	}
}

// newNetworkLogConfig returns a zeronetwork logger config.
// The suite injects the correct log-server port into RemoteLogParams before
// creating the logger, so no hardcoded port is needed here.
func newNetworkLogConfig() *logger.Config {
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

func newConsoleLogConfig() *logger.Config {
	return &logger.Config{
		Kind:         "zerolog",
		ConsoleColor: false,
		Console:      true,
		ConsoleLevel: "trace",
		FileLevel:    "trace",
		NetworkLevel: "trace",
	}
}
