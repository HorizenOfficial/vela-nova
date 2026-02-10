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
	expectedBalance := new(app.Uint256).SetBytes(depositAmount.Bytes())
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
	transferValue := app.NewUint256(500_000_000_000_000_000)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	state, _, fuel, failure := runtime.Deposit(ctx, appId, ethSender, depositAmount, state, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	recAddress, err := app.HexToAddress(recipientHex)
	require.NoError(t, err)

	payload := app.PayloadInstructions{
		Type:     "transfer",
		Transfer: &app.TransferInstruction{To: recAddress, Amount: transferValue},
	}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	newState, events, withdrawals, _, fuel, failure := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 2)
	require.Len(t, withdrawals, 0)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(newState, &stateData))
	expectedBalance := app.NewUint256(0)
	expectedBalance.Sub(*new(app.Uint256).SetBytes(depositAmount.Bytes()), *transferValue)
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
	withdrawValue := app.NewUint256(500_000_000_000_000_000)
	withdrawAddrHex := "0x1234567890123456789012345678901234567890"
	ethWithdrawAddr := ethCommon.HexToAddress(withdrawAddrHex)
	withdrawAddress, err := app.HexToAddress(withdrawAddrHex)
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

	newState, events, withdrawals, _, fuel, failure := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 1)
	require.Len(t, withdrawals, 1)
	assert.Equal(t, ethWithdrawAddr, withdrawals[0].DestinationAddress)
	assert.Equal(t, withdrawValue.String(), withdrawals[0].Amount.String())
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(newState, &stateData))
	expectedBalance := app.NewUint256(0)
	expectedBalance.Sub(*new(app.Uint256).SetBytes(depositAmount.Bytes()), *withdrawValue)
	assert.Equal(t, expectedBalance.String(), stateData.Accounts[senderHex].Balance.String())
}

func TestIntegration_ProcessRequest_Deanonymize(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger())
	defer runtime.Close()

	type reportStruct struct {
		Accounts map[ethCommon.Address]*app.AccountState `json:"accounts"`
		Nonce    uint64                                  `json:"nonce"`
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

	_, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, sender, common.Deanonymize, []byte("{}"), state, wasmBytes)
	require.Nil(t, failure)
	require.NotNil(t, reportBytes)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(20)))

	var report reportStruct
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	require.Contains(t, report.Accounts, sender)
	expectedBalance := new(app.Uint256).SetBytes(depositAmount.Bytes())
	assert.Equal(t, expectedBalance.String(), report.Accounts[sender].Balance.String())
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
