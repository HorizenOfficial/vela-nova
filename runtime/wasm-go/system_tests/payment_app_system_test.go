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

	"github.com/HorizenOfficial/vela-common-go/wasm/types"
	"github.com/HorizenOfficial/vela-nova/payment-app/app"
	"github.com/HorizenOfficial/vela/pkg/authorityservice/deployartifact"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
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

// hostSenderEvent mirrors the guest SenderEvent emitted to the sender of a
// private transfer. Balance is the sender's balance AFTER the transfer.
type hostSenderEvent struct {
	Type         string            `json:"type"`
	To           ethCommon.Address `json:"to"`
	TokenAddress ethCommon.Address `json:"tokenAddress"`
	Amount       *common.Big       `json:"amount"`
	Balance      *common.Big       `json:"balance"`
	Nonce        uint64            `json:"nonce"`
	InvoiceID    string            `json:"invoice_id,omitempty"`
}

// hostRecipientEvent mirrors the guest RecipientEvent emitted to the recipient
// of a private transfer. Balance is the recipient's balance AFTER the transfer.
type hostRecipientEvent struct {
	Type         string            `json:"type"`
	From         ethCommon.Address `json:"from"`
	TokenAddress ethCommon.Address `json:"tokenAddress"`
	Amount       *common.Big       `json:"amount"`
	Balance      *common.Big       `json:"balance"`
	Nonce        uint64            `json:"nonce"`
	InvoiceID    string            `json:"invoice_id,omitempty"`
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
	ctorBytes, err := json.Marshal(app.DeployParams{AllowedTokens: resolved})
	require.NoError(t, err)
	descriptor.ConstructorParams = ctorBytes

	payload, err := json.Marshal(descriptor)
	require.NoError(t, err)
	return payload
}

// deployAppWithTokens submits a Deploy request for appID with the given allowed-tokens
// list (empty = ETH only, no ConstructorParams) and waits for the app to appear in
// both the manager DB and the mock blockchain.
func deployAppWithTokens(t *testing.T, suite *systemTests.SystemTestSuite, appID common.ApplicationIdType, sender ethCommon.Address, wasmBytecode []byte, allowedTokens []ethCommon.Address, timeout time.Duration) {
	t.Helper()
	deployReq := &common.Request{
		RequestType:   common.Deploy,
		ApplicationID: appID,
		RequestID:     commontestutil.GenerateRandomRequestID(),
		Payload:       buildDeployDescriptorWithTokens(t, suite, wasmBytecode, allowedTokens),
		Sender:        sender,
		Timestamp:     common.ToBig(new(big.Int).SetInt64(time.Now().Unix())),
		AssetAmount:   common.NewBig(0),
		MaxFeeValue:   common.NewBig(100),
	}
	require.NoError(t, suite.SubmitRequest(deployReq))
	_, err := suite.WaitForAppStateInDB(appID, timeout)
	require.NoError(t, err)
	_, err = suite.WaitForAppStateInBlockchain(appID, timeout)
	require.NoError(t, err)
}

// registerSeedUser generates a user key and submits an AssociateKey request that
// embeds the encrypted secp256k1 seed — events to this user are routed via the
// hashed-subtype scheme (WaitForEventBySubtypes with AllSubtypes(seed, N)).
func registerSeedUser(t *testing.T, suite *systemTests.SystemTestSuite, cryptoHelper *systemTests.CryptoHelper, executorPubKey *cryptotypes.PublicKeyP521, appID common.ApplicationIdType, user ethCommon.Address, timeout time.Duration) {
	t.Helper()
	userKey, err := cryptoHelper.GenerateUserKey(user)
	require.NoError(t, err)
	reqID := commontestutil.GenerateRandomRequestID()
	req, err := cryptoHelper.CreateAssociateKeyRequest(appID, reqID, user, userKey.PublicKey(), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(req))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))
}

// registerNoSeedUser submits an AssociateKey request carrying only the 133-byte
// P521 public key (no encrypted seed). The framework preserves the WASM-provided
// plaintext subtype when routing events to this user.
func registerNoSeedUser(t *testing.T, suite *systemTests.SystemTestSuite, cryptoHelper *systemTests.CryptoHelper, appID common.ApplicationIdType, user ethCommon.Address, timeout time.Duration) {
	t.Helper()
	userKey, err := cryptoHelper.GenerateUserKey(user)
	require.NoError(t, err)
	reqID := commontestutil.GenerateRandomRequestID()
	req := &common.Request{
		ApplicationID: appID,
		RequestID:     reqID,
		RequestType:   common.AssociateKey,
		Payload:       userKey.PublicKey().Bytes(), // 133 bytes, no encrypted seed
		Sender:        user,
		Timestamp:     common.ToBig(new(big.Int).SetInt64(time.Now().Unix())),
		AssetAmount:   common.NewBig(0),
		TokenAddress:  ethCommon.Address{},
		MaxFeeValue:   common.NewBig(100),
	}
	require.NoError(t, suite.SubmitRequest(req))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))
}

// fetchDeanonAccounts submits a deanonymization request from `auditor`, waits
// for the report, decrypts it, asserts the envelope's appID and requestID
// round-trip correctly, and returns the parsed accounts map.
func fetchDeanonAccounts(t *testing.T, suite *systemTests.SystemTestSuite, cryptoHelper *systemTests.CryptoHelper, executorPubKey *cryptotypes.PublicKeyP521, appID common.ApplicationIdType, auditor ethCommon.Address, timeout time.Duration) map[string]interface{} {
	t.Helper()
	reqID := commontestutil.GenerateRandomRequestID()
	req, err := cryptoHelper.CreateDeanonymizationRequest(appID, reqID, auditor, []byte("{}"), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(req))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	report, err := suite.WaitForDeanonymizationReport(reqID, timeout)
	require.NoError(t, err)
	require.NotNil(t, report)

	decrypted, err := cryptoHelper.DecryptDeanonymizationReport(auditor, report, executorPubKey)
	require.NoError(t, err)

	var envelope struct {
		ApplicationId   common.ApplicationIdType `json:"applicationId"`
		RequestId       common.RequestIdType     `json:"requestId"`
		ReportDataBytes interface{}              `json:"reportDataBytes"`
	}
	require.NoError(t, json.Unmarshal(decrypted, &envelope))
	require.Equal(t, appID, envelope.ApplicationId, "deanon report envelope appID must round-trip")
	require.Equal(t, reqID, envelope.RequestId, "deanon report envelope requestID must round-trip")

	jsonStr, ok := envelope.ReportDataBytes.(string)
	require.True(t, ok, "reportDataBytes is not a string")
	reportBytes, err := base64.StdEncoding.DecodeString(jsonStr)
	require.NoError(t, err, "reportDataBytes is not base64 encoded")

	var data map[string]interface{}
	require.NoError(t, json.Unmarshal(reportBytes, &data))
	accounts, ok := data["accounts"].(map[string]interface{})
	require.True(t, ok, "accounts is not a map")
	return accounts
}

// parseReportBalance extracts a 0x-prefixed hex balance string for tokenHex from
// a deanon report's per-account balances map and parses it into a *big.Int.
// The balance MUST be present — use this when the test expects a positive entry.
func parseReportBalance(t *testing.T, balances map[string]interface{}, tokenHex string) *big.Int {
	t.Helper()
	s, ok := balances[tokenHex].(string)
	require.True(t, ok, "balance for token %s is missing or not a string", tokenHex)
	require.True(t, strings.HasPrefix(s, "0x"), "balance not hex-prefixed: %s", s)
	v, ok := new(big.Int).SetString(s[2:], 16)
	require.True(t, ok, "failed to parse balance hex: %s", s)
	return v
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

	// Deploy the application (ETH-only: no ConstructorParams)
	deployAppWithTokens(t, suite, appID, userAddress, wasmBytecode, nil, timeout)

	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	// Register user and auditor keys (both seed-registered)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, userAddress, timeout)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)

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
	registerNoSeedUser(t, suite, cryptoHelper, appID, noSeedUser, timeout)

	// Deposit 1 ETH for the no-seed user; plaintext "deposit" subtype should match
	noSeedDepositAmount := big.NewInt(1000000000000000000)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), noSeedUser, ethCommon.Address{}, noSeedDepositAmount, false)

	// Deanonymization report as auditor — verifies final state after deposit and withdrawal.
	// Verify balances: seed user (2 ETH - 0.5 ETH = 1.5 ETH) and no-seed user (1 ETH)
	accounts := fetchDeanonAccounts(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)
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
		balance := parseReportBalance(t, balances, ethTokenHex)

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
	deployAppWithTokens(t, suite, appID, userAddress, wasmBytecode, []ethCommon.Address{tokenAddress}, timeout)

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
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, userAddress, timeout)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)

	// --- Deposit the ERC-20 token ---
	depositAmount := big.NewInt(1_000_000) // arbitrary; token decimals are irrelevant here
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, tokenAddress, depositAmount, true)

	// --- Withdraw a portion ---
	withdrawAmount := big.NewInt(400_000)
	withdrawFromPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, recipientAddress, tokenAddress, withdrawAmount)

	// --- Deanonymization report: remaining balance keyed under the token ---
	accounts := fetchDeanonAccounts(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)
	require.Len(t, accounts, 1, "expected exactly one account in report")

	expectedRemaining := new(big.Int).Sub(depositAmount, withdrawAmount)
	for addrHex, acct := range accounts {
		acctMap, ok := acct.(map[string]interface{})
		require.True(t, ok, "account entry is not a map")
		balances, ok := acctMap["balances"].(map[string]interface{})
		require.True(t, ok, "balances is not a map")

		// Remaining balance must be keyed under the ERC-20 token, not ETH.
		parsed := parseReportBalance(t, balances, tokenHex)
		require.Equal(t, 0, expectedRemaining.Cmp(parsed),
			"account %s: expected token balance %s, got %s", addrHex, expectedRemaining, parsed)

		// No ETH deposit was made; any ETH entry must be zero.
		if ethBalanceStr, hasETH := balances[ethHex].(string); hasETH {
			ethParsed, ok := new(big.Int).SetString(ethBalanceStr[2:], 16)
			require.True(t, ok, "failed to parse ETH balance hex")
			require.Equal(t, 0, ethParsed.Sign(),
				"account %s should have no ETH balance, got %s", addrHex, ethBalanceStr)
		}
	}
}

// TestPaymentAppERC20MultiToken deploys the app with two ERC-20 tokens
// allowlisted, deposits both from the same user, withdraws only one, and
// verifies via the deanonymization report that per-token balances are
// isolated: the withdrawn token shows (deposit - withdrawal), the other
// shows its original deposit untouched.
func TestPaymentAppERC20MultiToken(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment-erc20-multi", newNetworkLogConfig(), newNetworkLogConfig())
	defer suite.Cleanup()

	wasmBytecode := buildAndLoadWasmModule(t)

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	appID := common.NewApplicationId(1)
	timeout := 100 * time.Second

	// Two synthetic ERC-20 token addresses. Not deployed contracts — see
	// scope comment at top of this section.
	tokenA := ethCommon.HexToAddress("0x000000000000000000000000000000000000A11A")
	tokenB := ethCommon.HexToAddress("0x000000000000000000000000000000000000B22B")
	recipientAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")

	cryptoHelper := systemTests.NewCryptoHelper()
	userAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)
	auditorAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)

	// --- Deploy with AllowedTokens = [tokenA, tokenB] ---
	deployAppWithTokens(t, suite, appID, userAddress, wasmBytecode, []ethCommon.Address{tokenA, tokenB}, timeout)

	tokenAHex := strings.ToLower(tokenA.Hex())
	tokenBHex := strings.ToLower(tokenB.Hex())

	// --- Register user and auditor keys ---
	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, userAddress, timeout)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)

	// --- Deposit both tokens from the same user ---
	depositA := big.NewInt(1_000_000)
	depositB := big.NewInt(5_500_000)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, tokenA, depositA, true)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, tokenB, depositB, true)

	// --- Withdraw tokenA only; the helper asserts the mock-chain Withdrawal
	//     record carries tokenA as its TokenAddress (not tokenB).
	withdrawA := big.NewInt(400_000)
	withdrawFromPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, recipientAddress, tokenA, withdrawA)

	// --- Deanonymization report: verify per-token isolation ---
	accounts := fetchDeanonAccounts(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)
	require.Len(t, accounts, 1, "expected exactly one account in report")

	expectedA := new(big.Int).Sub(depositA, withdrawA) // tokenA was withdrawn partially
	expectedB := new(big.Int).Set(depositB)            // tokenB was NOT touched

	for addrHex, acct := range accounts {
		acctMap, ok := acct.(map[string]interface{})
		require.True(t, ok, "account entry is not a map")
		balances, ok := acctMap["balances"].(map[string]interface{})
		require.True(t, ok, "balances is not a map")

		// tokenA: deposit - withdrawal
		parsedA := parseReportBalance(t, balances, tokenAHex)
		require.Equal(t, 0, expectedA.Cmp(parsedA),
			"account %s tokenA balance: expected %s, got %s", addrHex, expectedA, parsedA)

		// tokenB: unchanged (the withdrawal of tokenA must not have affected tokenB)
		parsedB := parseReportBalance(t, balances, tokenBHex)
		require.Equal(t, 0, expectedB.Cmp(parsedB),
			"account %s tokenB balance should be unchanged: expected %s, got %s", addrHex, expectedB, parsedB)
	}
}

// TestPaymentAppERC20MultiUser exercises a private transfer between two users
// in the ERC-20 token. The two users are deliberately in different event-
// routing modes to cover both within a single test:
//
//   - userA is SEED-REGISTERED: AssociateKey request carries an encrypted
//     seed, and events reach userA via the privacy-preserving hashed-subtype
//     set (WaitForEventBySubtypes with AllSubtypes(seed, N)).
//   - userB is NO-SEED: AssociateKey request carries only the P521 public
//     key (133 bytes, no encrypted seed). Events reach userB via the plain
//     subtype the guest emits (WaitForEvent with the literal subtype name).
//
// The transfer goes A -> B, so we verify:
//   - userA receives a "transfer_sent" event via the seed path, with correct
//     tokenAddress, amount, and post-transfer balance
//   - userB receives a "transfer_received" event via the no-seed plain-
//     subtype path, with correct tokenAddress, amount, and post-transfer
//     balance
//   - the deanonymization report shows both accounts with the expected
//     balances
//   - total conservation: (userA remaining + userB remaining) == sum of the
//     two original deposits (no tokens minted or lost by the transfer)
//
// The no-seed user receiving a transfer_received event is new coverage vs
// TestPaymentAppFullFlow, which only exercises the no-seed path for deposit
// events.
func TestPaymentAppERC20MultiUser(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment-erc20-multiuser", newNetworkLogConfig(), newNetworkLogConfig())
	defer suite.Cleanup()

	wasmBytecode := buildAndLoadWasmModule(t)

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	appID := common.NewApplicationId(1)
	timeout := 100 * time.Second

	tokenAddress := ethCommon.HexToAddress("0x000000000000000000000000000000000000C0DE")

	cryptoHelper := systemTests.NewCryptoHelper()
	userA, err := cryptoHelper.GenerateUserIdentity() // seed-registered
	require.NoError(t, err)
	userB, err := cryptoHelper.GenerateUserIdentity() // no-seed
	require.NoError(t, err)
	auditorAddress, err := cryptoHelper.GenerateUserIdentity() // seed-registered (needs to decrypt deanon report)
	require.NoError(t, err)

	// --- Deploy with AllowedTokens = [tokenAddress] ---
	deployAppWithTokens(t, suite, appID, userA, wasmBytecode, []ethCommon.Address{tokenAddress}, timeout)

	tokenHex := strings.ToLower(tokenAddress.Hex())

	// --- Register keys ---
	// userA and auditor: seed-registered (CreateAssociateKeyRequest embeds the
	// encrypted seed in the request payload). userB: no-seed (P521 key only).
	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, userA, timeout)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)
	registerNoSeedUser(t, suite, cryptoHelper, appID, userB, timeout)

	// --- Both users deposit the ERC-20 token (independent amounts) ---
	// useSeed=true for userA -> event matched via hashed subtype set
	// useSeed=false for userB -> event matched via plain "deposit" subtype
	depositA := big.NewInt(3_000_000)
	depositB := big.NewInt(2_000_000)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userA, tokenAddress, depositA, true)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userB, tokenAddress, depositB, false)

	// --- Private transfer A -> B ---
	transferAmount := big.NewInt(1_200_000)

	// Use the guest's real PayloadInstructions / TransferInstruction types so
	// a field rename in app/types.go breaks compilation instead of the wire format.
	transferPayload, err := json.Marshal(app.PayloadInstructions{
		Type: "transfer",
		Transfer: &app.TransferInstruction{
			To:           types.Address(userB),
			TokenAddress: types.Address(tokenAddress),
			Amount:       new(types.Uint256).SetBytes(transferAmount.Bytes()),
		},
	})
	require.NoError(t, err)

	transferReqID := commontestutil.GenerateRandomRequestID()
	transferReq, err := cryptoHelper.CreateProcessRequest(appID, transferReqID, userA, transferPayload, executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(transferReq))
	require.NoError(t, suite.AssertRequestCompleted(transferReqID, timeout))

	// --- Verify sender event (userA, seed path) ---
	userASeed, err := cryptoHelper.ComputeSeed(userA)
	require.NoError(t, err)
	senderEvent, err := suite.WaitForEventBySubtypes(userA, executor.AllSubtypes(userASeed, executor.DefaultSubtypeN), timeout)
	require.NoError(t, err)
	senderDecrypted, err := cryptoHelper.DecryptEvent(userA, senderEvent, executorPubKey)
	require.NoError(t, err)
	var senderData hostSenderEvent
	require.NoError(t, json.Unmarshal(senderDecrypted, &senderData))
	require.Equal(t, userB, senderData.To, "sender event must record recipient=userB")
	require.Equal(t, tokenAddress, senderData.TokenAddress,
		"sender event must carry tokenAddress")
	require.Equal(t, 0, transferAmount.Cmp(senderData.Amount.ToInt()),
		"sender event amount mismatch")
	expectedABalance := new(big.Int).Sub(depositA, transferAmount)
	require.Equal(t, 0, expectedABalance.Cmp(senderData.Balance.ToInt()),
		"userA post-transfer balance: expected %s, got %s", expectedABalance, senderData.Balance.ToInt())

	// --- Verify recipient event (userB, no-seed plain-subtype path) ---
	// The guest emits the recipient event with subtype "transfer_received";
	// with no seed registered the framework preserves that literal subtype.
	recipientEvent, err := suite.WaitForEvent(userB, "transfer_received", timeout)
	require.NoError(t, err)
	recipientDecrypted, err := cryptoHelper.DecryptEvent(userB, recipientEvent, executorPubKey)
	require.NoError(t, err)
	var recipientData hostRecipientEvent
	require.NoError(t, json.Unmarshal(recipientDecrypted, &recipientData))
	require.Equal(t, userA, recipientData.From, "recipient event must record sender=userA")
	require.Equal(t, tokenAddress, recipientData.TokenAddress,
		"recipient event must carry tokenAddress")
	require.Equal(t, 0, transferAmount.Cmp(recipientData.Amount.ToInt()),
		"recipient event amount mismatch")
	expectedBBalance := new(big.Int).Add(depositB, transferAmount)
	require.Equal(t, 0, expectedBBalance.Cmp(recipientData.Balance.ToInt()),
		"userB post-transfer balance: expected %s, got %s", expectedBBalance, recipientData.Balance.ToInt())

	// --- Deanonymization report: both accounts with correct balances ---
	accounts := fetchDeanonAccounts(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)
	require.Len(t, accounts, 2, "expected exactly two accounts (userA + userB)")

	// Walk both accounts and verify per-user balance. Also tally the total
	// for conservation check.
	total := new(big.Int)
	seenA, seenB := false, false
	for addrHex, acct := range accounts {
		acctMap := acct.(map[string]interface{})
		balances := acctMap["balances"].(map[string]interface{})
		bal := parseReportBalance(t, balances, tokenHex)
		total.Add(total, bal)

		addr := ethCommon.HexToAddress(addrHex)
		switch addr {
		case userA:
			seenA = true
			require.Equal(t, 0, expectedABalance.Cmp(bal),
				"userA report balance: expected %s, got %s", expectedABalance, bal)
		case userB:
			seenB = true
			require.Equal(t, 0, expectedBBalance.Cmp(bal),
				"userB report balance: expected %s, got %s", expectedBBalance, bal)
		default:
			t.Fatalf("unexpected account %s in report", addrHex)
		}
	}
	require.True(t, seenA, "userA missing from report")
	require.True(t, seenB, "userB missing from report")

	// Conservation: total across accounts must equal the sum of the two
	// original deposits (no tokens minted or lost by the transfer).
	expectedTotal := new(big.Int).Add(depositA, depositB)
	require.Equal(t, 0, expectedTotal.Cmp(total),
		"total conservation broken: expected %s, got %s", expectedTotal, total)
}

// TestPaymentAppERC20NegativePath is a table-driven test covering the WASM-
// layer rejections for ERC-20 operations. It runs a single suite with one
// successful baseline deposit, then a series of subtests each submitting a
// request that the guest should reject.
//
// The guest's validation happens BEFORE any state mutation, so all four
// failing subtests must leave state untouched. A final successful deposit
// after the failures proves the app remains functional, and the closing
// deanonymization report asserts the balance equals the baseline plus the
// final deposit (nothing leaked through the failed operations).
//
// Scope note: all subtests exercise guest-side rejections visible to the
// test via AssertRequestCompleted returning "has failed". The mock blockchain
// stores failed requests but not their UpdatePayload, so this test does not
// assert on specific guest error strings — only that the request failed and
// that state is intact after each failure. Contract-layer rejections (bad
// permit, wrong allowance, reverts inside submitRequest) are a separate path
// and belong to tests against a simulated chain.
func TestPaymentAppERC20NegativePath(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment-erc20-neg", newNetworkLogConfig(), newNetworkLogConfig())
	defer suite.Cleanup()

	wasmBytecode := buildAndLoadWasmModule(t)

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	appID := common.NewApplicationId(1)
	timeout := 100 * time.Second

	allowedToken := ethCommon.HexToAddress("0x000000000000000000000000000000000000A11A")
	disallowedToken := ethCommon.HexToAddress("0x000000000000000000000000000000000000BAD1")
	recipientAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")

	cryptoHelper := systemTests.NewCryptoHelper()
	userA, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)
	userB, err := cryptoHelper.GenerateUserIdentity() // registered but never deposits
	require.NoError(t, err)
	auditorAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)

	// --- Deploy: only allowedToken is in the allowlist ---
	deployAppWithTokens(t, suite, appID, userA, wasmBytecode, []ethCommon.Address{allowedToken}, timeout)

	tokenHex := strings.ToLower(allowedToken.Hex())

	// --- Register all three keys (all seed-registered for simplicity) ---
	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, userA, timeout)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, userB, timeout)
	registerSeedUser(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)

	// --- Baseline: userA deposits 1000 of allowedToken (succeeds) ---
	baselineAmount := big.NewInt(1000)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userA, allowedToken, baselineAmount, true)

	// --- Negative subtests. Each must fail and leave state unchanged. ---

	t.Run("deposit with disallowed token", func(t *testing.T) {
		reqID := commontestutil.GenerateRandomRequestID()
		req, err := cryptoHelper.CreateTokenDepositRequest(appID, reqID, userA, disallowedToken, big.NewInt(500), executorPubKey)
		require.NoError(t, err)
		require.NoError(t, suite.SubmitRequest(req))
		err = suite.AssertRequestCompleted(reqID, timeout)
		require.Error(t, err, "deposit of disallowed token must fail")
		require.Contains(t, err.Error(), "has failed")
	})

	t.Run("withdrawal exceeding balance", func(t *testing.T) {
		reqID := commontestutil.GenerateRandomRequestID()
		// userA has baselineAmount (1000); withdraw more than that.
		tooMuch := new(big.Int).Mul(baselineAmount, big.NewInt(2))
		req, err := cryptoHelper.CreateTokenWithdrawalRequest(appID, reqID, userA, recipientAddress, allowedToken, common.ToBig(tooMuch), executorPubKey)
		require.NoError(t, err)
		require.NoError(t, suite.SubmitRequest(req))
		err = suite.AssertRequestCompleted(reqID, timeout)
		require.Error(t, err, "withdrawal exceeding balance must fail")
		require.Contains(t, err.Error(), "has failed")
	})

	t.Run("transfer exceeding balance", func(t *testing.T) {
		tooMuch := new(big.Int).Mul(baselineAmount, big.NewInt(2))
		transferPayload, err := json.Marshal(app.PayloadInstructions{
			Type: "transfer",
			Transfer: &app.TransferInstruction{
				To:           types.Address(userB),
				TokenAddress: types.Address(allowedToken),
				Amount:       new(types.Uint256).SetBytes(tooMuch.Bytes()),
			},
		})
		require.NoError(t, err)
		reqID := commontestutil.GenerateRandomRequestID()
		req, err := cryptoHelper.CreateProcessRequest(appID, reqID, userA, transferPayload, executorPubKey)
		require.NoError(t, err)
		require.NoError(t, suite.SubmitRequest(req))
		err = suite.AssertRequestCompleted(reqID, timeout)
		require.Error(t, err, "transfer exceeding balance must fail")
		require.Contains(t, err.Error(), "has failed")
	})

	t.Run("transfer from account with no deposits", func(t *testing.T) {
		// userB is registered but has never deposited — their account entry
		// in state does not exist. Transfer from userB must fail with
		// "Account does not exist!".
		transferPayload, err := json.Marshal(app.PayloadInstructions{
			Type: "transfer",
			Transfer: &app.TransferInstruction{
				To:           types.Address(userA),
				TokenAddress: types.Address(allowedToken),
				Amount:       new(types.Uint256).SetBytes(big.NewInt(100).Bytes()),
			},
		})
		require.NoError(t, err)
		reqID := commontestutil.GenerateRandomRequestID()
		req, err := cryptoHelper.CreateProcessRequest(appID, reqID, userB, transferPayload, executorPubKey)
		require.NoError(t, err)
		require.NoError(t, suite.SubmitRequest(req))
		err = suite.AssertRequestCompleted(reqID, timeout)
		require.Error(t, err, "transfer from non-existent account must fail")
		require.Contains(t, err.Error(), "has failed")
	})

	// --- Post-condition: state survived every failure. ---
	// A successful deposit confirms the app is still operational.
	finalDeposit := big.NewInt(50)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userA, allowedToken, finalDeposit, true)

	// Deanonymization report: userA's balance must equal baseline + final
	// deposit, nothing more and nothing less. If any of the failed subtests
	// had partially mutated state, this assertion would catch it.
	expected := new(big.Int).Add(baselineAmount, finalDeposit)

	accounts := fetchDeanonAccounts(t, suite, cryptoHelper, executorPubKey, appID, auditorAddress, timeout)
	require.Len(t, accounts, 1, "only userA should have an account (userB never deposited)")

	for addrHex, acct := range accounts {
		addr := ethCommon.HexToAddress(addrHex)
		require.Equal(t, userA, addr, "unexpected account %s in report", addrHex)

		balances := acct.(map[string]interface{})["balances"].(map[string]interface{})
		parsed := parseReportBalance(t, balances, tokenHex)
		require.Equal(t, 0, expected.Cmp(parsed),
			"userA balance after all failures + final deposit: expected %s, got %s (state was mutated by a failed operation)",
			expected, parsed)

		// disallowedToken must not appear anywhere — the failed deposit
		// of it must not have leaked a zero-balance entry into state.
		disallowedHex := strings.ToLower(disallowedToken.Hex())
		_, leaked := balances[disallowedHex]
		require.False(t, leaked,
			"disallowed-token balance entry must not appear in state, but found key %s", disallowedHex)
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
