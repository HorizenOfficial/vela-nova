package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"testing"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/horizen-cce-common-go/wasm/types"
	"github.com/horizen-pes-nova/payment-app/app"
	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/logger"
	"github.com/horizen-pes/pkg/wasm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readWasm(t *testing.T) []byte {
	t.Helper()

	wasmModulePath := "build/payment_app.wasm"

	cmd := exec.Command("make", "build")
	cmd.Dir = "." // Run in the current directory
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build wasm module: %s", string(output))

	// Read the wasm module
	wasmBytes, err := os.ReadFile(wasmModulePath)
	require.NoError(t, err)
	require.NotEmpty(t, wasmBytes)
	return wasmBytes
}

func TestIntegration_LoadModule(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(state, &stateData))
	assert.Equal(t, appId, common.ApplicationIdType(stateData.AppID))
}

func TestIntegration_Deposit(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	senderHex := fmt.Sprintf("0xadd%037x", 1)
	ethSender := ethCommon.HexToAddress(senderHex)
	depositAmount := big.NewInt(1_000_000_000_000_000_000)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	newState, events, fuel, failure := runtime.Deposit(ctx, appId, ethSender, depositAmount, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 1)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(newState, &stateData))
	require.Contains(t, stateData.Accounts, senderHex)
	expectedBalance := new(types.Uint256).SetBytes(depositAmount.Bytes())
	assert.Equal(t, expectedBalance.String(), stateData.Accounts[senderHex].Balance.String())
}

func TestIntegration_ProcessRequest_Transfer(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	senderHex := fmt.Sprintf("0xadd%037x", 1)
	ethSender := ethCommon.HexToAddress(senderHex)
	recipientHex := fmt.Sprintf("0xadd%037x", 2)
	depositAmount := big.NewInt(2_000_000_000_000_000_000)
	transferValue := types.NewUint256(500_000_000_000_000_000)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	state, _, fuel, failure := runtime.Deposit(ctx, appId, ethSender, depositAmount, state, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	recAddress, err := types.HexToAddress(recipientHex)
	require.NoError(t, err)

	payload := app.PayloadInstructions{
		Type:     "transfer",
		Transfer: &app.TransferInstruction{To: recAddress, Amount: transferValue},
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	newState, events, withdrawals, fuel, failure := runtime.ProcessRequest(ctx, appId, ethSender, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 2)
	require.Len(t, withdrawals, 0)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(newState, &stateData))
	expectedBalance := types.NewUint256(0)
	expectedBalance.Sub(*new(types.Uint256).SetBytes(depositAmount.Bytes()), *transferValue)
	assert.Equal(t, expectedBalance.String(), stateData.Accounts[senderHex].Balance.String())
	assert.Equal(t, transferValue.String(), stateData.Accounts[recipientHex].Balance.String())
}

func TestIntegration_ProcessRequest_Withdrawal(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	senderHex := fmt.Sprintf("0xadd%037x", 1)
	ethSender := ethCommon.HexToAddress(senderHex)
	depositAmount := big.NewInt(1_000_000_000_000_000_000)
	withdrawValue := types.NewUint256(500_000_000_000_000_000)
	withdrawAddrHex := "0x1234567890123456789012345678901234567890"
	ethWithdrawAddr := ethCommon.HexToAddress(withdrawAddrHex)
	withdrawAddress, err := types.HexToAddress(withdrawAddrHex)
	require.NoError(t, err)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	state, _, fuel, failure := runtime.Deposit(ctx, appId, ethSender, depositAmount, state, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	payload := app.PayloadInstructions{
		Type:     "withdraw",
		Withdraw: &app.WithdrawInstruction{To: withdrawAddress, Amount: withdrawValue},
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	newState, events, withdrawals, fuel, failure := runtime.ProcessRequest(ctx, appId, ethSender, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 1)
	require.Len(t, withdrawals, 1)
	assert.Equal(t, ethWithdrawAddr, withdrawals[0].DestinationAddress)
	assert.Equal(t, withdrawValue.String(), withdrawals[0].Amount.String())
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(newState, &stateData))
	expectedBalance := types.NewUint256(0)
	expectedBalance.Sub(*new(types.Uint256).SetBytes(depositAmount.Bytes()), *withdrawValue)
	assert.Equal(t, expectedBalance.String(), stateData.Accounts[senderHex].Balance.String())
}

func TestIntegration_GenerateDeanonymizationReport(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	type reportStruct struct {
		ApplicationID string                                  `json:"applicationId"`
		RequestID     string                                  `json:"requestId"`
		Accounts      map[ethCommon.Address]*app.AccountState `json:"accounts"`
		Nonce         uint64                                  `json:"nonce"`
	}

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	depositAmount := big.NewInt(1_000_000_000_000_000_000)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	state, _, fuel, failure := runtime.Deposit(ctx, appId, sender, depositAmount, state, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	reportBytes, fuel, failure := runtime.GenerateDeanonymizationReport(ctx, appId, []byte("{}"), state, wasmBytes)
	require.Nil(t, failure)
	require.NotNil(t, reportBytes)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(20)))

	var report reportStruct
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.Contains(t, report.Accounts, sender)
	expectedBalance := new(types.Uint256).SetBytes(depositAmount.Bytes())
	assert.Equal(t, expectedBalance.String(), report.Accounts[sender].Balance.String())
}

// requireMemoryClean checks that guest memory is fully deallocated.
func requireMemoryClean(t *testing.T, runtime *wasm.WasmtimeRuntime, appId common.ApplicationIdType, wasmBytes []byte, msgAndArgs ...interface{}) {
	t.Helper()
	ctx := context.Background()
	mapEntries, totalBytes, err := runtime.GetAllocatedMemoryStats2(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, int64(0), mapEntries, msgAndArgs...)
	require.Equal(t, int64(0), totalBytes, msgAndArgs...)
}

// TestIntegration_MemoryCleanBetweenOps verifies that BytesToPtr allocations
// (created by SerializeAndWriteResult inside the WASM guest) are fully deallocated
// after each host call.
func TestIntegration_MemoryCleanBetweenOps(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	senderHex := fmt.Sprintf("0xadd%037x", 1)
	ethSender := ethCommon.HexToAddress(senderHex)
	recipientHex := fmt.Sprintf("0xadd%037x", 2)
	recipientAddress, err := types.HexToAddress(recipientHex)
	require.NoError(t, err)
	withdrawAddrHex := "0x1234567890123456789012345678901234567890"
	withdrawAddress, err := types.HexToAddress(withdrawAddrHex)
	require.NoError(t, err)

	// LoadModule
	state, _, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after LoadModule")

	// Deposit
	state, _, _, failure := runtime.Deposit(ctx, appId, ethSender, big.NewInt(5_000_000_000_000_000_000), state, wasmBytes)
	require.Nil(t, failure)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after Deposit")

	// Transfer
	transferPayload := app.PayloadInstructions{
		Type:     "transfer",
		Transfer: &app.TransferInstruction{To: recipientAddress, Amount: types.NewUint256(100)},
	}
	transferBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)

	state, _, _, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, transferBytes, state, wasmBytes)
	require.Nil(t, failure2)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after ProcessRequest (transfer)")

	// Withdraw
	withdrawPayload := app.PayloadInstructions{
		Type:     "withdraw",
		Withdraw: &app.WithdrawInstruction{To: withdrawAddress, Amount: types.NewUint256(50)},
	}
	withdrawBytes, err := json.Marshal(withdrawPayload)
	require.NoError(t, err)

	state, _, _, _, failure2 = runtime.ProcessRequest(ctx, appId, ethSender, withdrawBytes, state, wasmBytes)
	require.Nil(t, failure2)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after ProcessRequest (withdraw)")

	// GenerateDeanonymizationReport
	_, _, failure = runtime.GenerateDeanonymizationReport(ctx, appId, []byte("{}"), state, wasmBytes)
	require.Nil(t, failure)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after GenerateDeanonymizationReport")
}

// TestIntegration_ErrorPathMemory verifies that error results returned by the guest
// (which still use SerializeAndWriteResult → BytesToPtr) do not leak memory.
func TestIntegration_ErrorPathMemory(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	senderHex := fmt.Sprintf("0xadd%037x", 1)
	ethSender := ethCommon.HexToAddress(senderHex)
	nonExistentUser := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 99))
	withdrawAddress, err := types.HexToAddress("0x1234567890123456789012345678901234567890")
	require.NoError(t, err)

	state, _, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	// Deposit so sender has a balance
	state, _, _, failure := runtime.Deposit(ctx, appId, ethSender, big.NewInt(100), state, wasmBytes)
	require.Nil(t, failure)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after initial deposit")

	// Error: withdraw more than balance
	payload := app.PayloadInstructions{
		Type:     "withdraw",
		Withdraw: &app.WithdrawInstruction{To: withdrawAddress, Amount: types.NewUint256(9999)},
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	_, _, _, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, payloadBytes, state, wasmBytes)
	require.NotNil(t, failure2, "expected error for insufficient balance")
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after insufficient balance error")

	// Error: transfer from non-existent account
	transferPayload := app.PayloadInstructions{
		Type:     "transfer",
		Transfer: &app.TransferInstruction{To: withdrawAddress, Amount: types.NewUint256(1)},
	}
	transferBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)

	_, _, _, _, failure2 = runtime.ProcessRequest(ctx, appId, nonExistentUser, transferBytes, state, wasmBytes)
	require.NotNil(t, failure2, "expected error for non-existent account")
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after non-existent account error")

	// Error: invalid state JSON
	_, _, _, failure = runtime.Deposit(ctx, appId, ethSender, big.NewInt(100), []byte("{bad-json}"), wasmBytes)
	require.NotNil(t, failure, "expected error for invalid state")
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after invalid state error")
}

// TestIntegration_LargeResultRoundTrip exercises BytesToPtr with a large JSON payload
// by creating many accounts and generating a report that serializes all of them.
func TestIntegration_LargeResultRoundTrip(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	state, _, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	// Create 100 accounts with deposits
	const numAccounts = 100
	for i := range numAccounts {
		addr := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", i))
		newState, _, _, failure := runtime.Deposit(ctx, appId, addr, big.NewInt(int64(1000+i)), state, wasmBytes)
		require.Nil(t, failure, "deposit failed for account %d", i)
		state = newState
	}

	// Verify large state
	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(state, &stateData))
	require.Len(t, stateData.Accounts, numAccounts)

	// Generate report with all accounts — large result through BytesToPtr
	reportBytes, _, failure := runtime.GenerateDeanonymizationReport(ctx, appId, []byte("{}"), state, wasmBytes)
	require.Nil(t, failure)
	require.NotNil(t, reportBytes)

	// Verify report contains all accounts
	var report app.UnencryptedDeanonymizationReportData
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.Len(t, report.Accounts, numAccounts)

	// No memory leaked despite large allocation
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after large result round-trip")
}

func newTestLogger() logger.Logger {
	testLogger := logger.NewLogger(
		&logger.Config{
			Kind:         "zerolog",
			ConsoleColor: false, // colors can print escape chars on tty
			Console:      true,
			ConsoleLevel: "trace",
			//FileName:     "qqq.log",
			//FileLevel:    "info",
		},
	)
	return testLogger
}
