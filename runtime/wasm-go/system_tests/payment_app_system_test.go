package main_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/HorizenOfficial/vela/pkg/common"
	commontestutil "github.com/HorizenOfficial/vela/pkg/common/testutil"
	"github.com/HorizenOfficial/vela/pkg/executor"
	"github.com/HorizenOfficial/vela/pkg/logger"
	systemTests "github.com/HorizenOfficial/vela/pkg/testutil"
	"github.com/stretchr/testify/require"
)

// host-side event types for test validation (app-specific, not framework types)
type hostDepositEvent struct {
	Type    string      `json:"type"`
	Amount  *common.Big `json:"amount"`
	Balance *common.Big `json:"balance"`
	Nonce   uint64      `json:"nonce"`
}

type hostWithdrawalEvent struct {
	Type    string            `json:"type"`
	To      ethCommon.Address `json:"to"`
	Amount  *common.Big       `json:"amount"`
	Balance *common.Big       `json:"balance"`
	Nonce   uint64            `json:"nonce"`
}

// depositToPaymentApp is a helper function to deposit funds and validate the deposit event.
func depositToPaymentApp(t *testing.T, suite *systemTests.SystemTestSuite, cryptoHelper *systemTests.CryptoHelper, appID common.ApplicationIdType, reqID common.RequestIdType, user ethCommon.Address, amount *big.Int) {
	t.Helper()
	timeout := 100 * time.Second

	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	depositReq, err := cryptoHelper.CreateDepositRequest(appID, reqID, user, amount, executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(depositReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	// vela v0.0.26: event subtypes are privacy-preserving (random HMAC derived from user seed).
	// Compute all possible subtypes for this user and wait for any of them.
	seed, err := cryptoHelper.ComputeSeed(user)
	require.NoError(t, err)
	depositEvent, err := suite.WaitForEventBySubtypes(user, executor.AllSubtypes(seed, executor.DefaultSubtypeN), timeout)
	require.NoError(t, err)
	decryptedData, err := cryptoHelper.DecryptEvent(user, depositEvent, executorPubKey)
	require.NoError(t, err)

	var eventData hostDepositEvent
	err = json.Unmarshal(decryptedData, &eventData)
	require.NoError(t, err)
	require.Equal(t, "deposit", eventData.Type)
	require.Equal(t, 0, amount.Cmp(eventData.Amount.ToInt()))

	executorSigningKey, err := suite.GetExecutorSigningKey()
	require.NoError(t, err)
	payload, err := suite.GetRequestUpdatePayload(reqID)
	require.NoError(t, err)
	err = cryptoHelper.ValidateUpdatePayloadSignature(payload, executorSigningKey)
	require.NoError(t, err)
}

// withdrawFromPaymentApp is a helper function to withdraw funds and validate the withdrawal event.
func withdrawFromPaymentApp(t *testing.T, suite *systemTests.SystemTestSuite, cryptoHelper *systemTests.CryptoHelper, appID common.ApplicationIdType, reqID common.RequestIdType, user, recipient ethCommon.Address, amount *big.Int) {
	t.Helper()
	timeout := 100 * time.Second

	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	withdrawalReq, err := cryptoHelper.CreateWithdrawalRequest(appID, reqID, user, recipient, common.ToBig(amount), executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(withdrawalReq))
	require.NoError(t, suite.AssertRequestCompleted(reqID, timeout))

	// vela v0.0.26: event subtypes are privacy-preserving (random HMAC derived from user seed).
	seed, err := cryptoHelper.ComputeSeed(user)
	require.NoError(t, err)
	withdrawalEvent, err := suite.WaitForEventBySubtypes(user, executor.AllSubtypes(seed, executor.DefaultSubtypeN), timeout)
	require.NoError(t, err)
	decryptedData, err := cryptoHelper.DecryptEvent(user, withdrawalEvent, executorPubKey)
	require.NoError(t, err)

	var eventData hostWithdrawalEvent
	err = json.Unmarshal(decryptedData, &eventData)
	require.NoError(t, err)
	require.Equal(t, "withdrawal", eventData.Type)
	require.Equal(t, recipient, eventData.To)
	require.Equal(t, 0, amount.Cmp(eventData.Amount.ToInt()))

	// Verify on-chain withdrawal was recorded
	withdrawal, err := suite.WaitForWithdrawal(appID, timeout)
	require.NoError(t, err)
	require.NotNil(t, withdrawal)
	require.Equal(t, recipient, withdrawal.DestinationAddress)
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


func TestPaymentAppFullFlow(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	// manager.LoadConfig() in vela v0.0.26 requires MANAGER_ARTIFACTS_PATH to be set.
	// NewSystemTestSuiteWithConfigs will override this with its own temp dir, but LoadConfig
	// must pass validation first.
	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment", newTestLogger(), newTestLogger())
	defer suite.Cleanup()

	wasmBytecode := buildAndLoadWasmModule(t)

	// vela v0.0.26: deploy uses artifact references. Store the wasm blob in the
	// suite's artifacts path and build the JSON descriptor payload.
	deployPayload := storeWasmArtifact(t, suite.GetArtifactsPath(), wasmBytecode)

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	appID := common.NewApplicationId(1)
	recipientAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")
	timeout := 100 * time.Second

	// vela v0.0.26: CreateAssociateKeyRequest requires a secp256k1 signing key whose
	// derived Ethereum address matches the sender. Use GenerateUserIdentity so the
	// address is derived from the key (rather than hardcoded hex addresses).
	cryptoHelper := systemTests.NewCryptoHelper()
	userAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)
	auditorAddress, err := cryptoHelper.GenerateUserIdentity()
	require.NoError(t, err)

	// Deploy the application
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

	// Deposit 2 ETH and validate event fields
	depositAmount := big.NewInt(2000000000000000000)
	depositToPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, depositAmount)

	// Withdraw 0.5 ETH and validate event fields
	withdrawAmount := big.NewInt(500000000000000000)
	withdrawFromPaymentApp(t, suite, cryptoHelper, appID, commontestutil.GenerateRandomRequestID(), userAddress, recipientAddress, withdrawAmount)

	// Deanonymization report as auditor — verifies final state after deposit and withdrawal
	executorPubKey, err = suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

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

	// Verify user balance reflects deposit minus withdrawal (2 ETH - 0.5 ETH = 1.5 ETH)
	accounts, ok := reportData["accounts"].(map[string]interface{})
	require.True(t, ok, "accounts is not a map")
	require.Len(t, accounts, 1, "expected exactly one account in report")

	expectedBalance := new(big.Int).Sub(depositAmount, withdrawAmount)
	for _, acct := range accounts {
		acctMap, ok := acct.(map[string]interface{})
		require.True(t, ok, "account entry is not a map")
		balanceStr, ok := acctMap["balance"].(string)
		require.True(t, ok, "balance is not a string")
		require.True(t, len(balanceStr) > 2 && balanceStr[:2] == "0x", "balance is not hex")
		balance, ok := new(big.Int).SetString(balanceStr[2:], 16)
		require.True(t, ok, "failed to parse balance hex")
		require.Equal(t, 0, expectedBalance.Cmp(balance),
			"expected balance %s, got %s", expectedBalance, balance)
	}
}

func newTestLogger() *logger.Config {
	return &logger.Config{
		Kind:         "zeronetwork",
		ConsoleColor: false, // colors can print escape chars on tty
		Console:      false,
		ConsoleLevel: "trace",
		//FileName:     "qqq.log",
		FileLevel:        "trace",
		RemoteLogParams:  common.TcpChannelConnectionParams{Ip: "localhost", Port: 5000},
		RemoteLogNetwork: "tcp",
		NetworkLevel:     "trace",
	}
}
