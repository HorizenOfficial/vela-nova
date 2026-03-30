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
	"github.com/HorizenOfficial/vela-common-go/wasm/types"
	"github.com/HorizenOfficial/vela-nova/payment-app/app"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/HorizenOfficial/vela/pkg/logger"
	"github.com/HorizenOfficial/vela/pkg/wasm"
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
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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

	// helper: build payload, execute transfer, verify state, return events
	doTransfer := func(t *testing.T, invoiceID string) []common.PlainEvent {
		t.Helper()
		payload := app.PayloadInstructions{
			Type:     "transfer",
			Transfer: &app.TransferInstruction{To: recAddress, Amount: transferValue, InvoiceID: invoiceID},
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)

		newState, events, withdrawals, _, fuel, failure := runtime.ProcessRequest(
			ctx, appId, ethSender, common.Process, payloadBytes, state, wasmBytes)
		require.Nil(t, failure)
		require.Len(t, events, 2)
		require.Len(t, withdrawals, 0)
		require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

		// Verify the state was updated
		var stateData app.ApplicationInternalState
		require.NoError(t, json.Unmarshal(newState, &stateData))
		expectedBalance := types.NewUint256(0)
		expectedBalance.Sub(*new(types.Uint256).SetBytes(depositAmount.Bytes()), *transferValue)
		assert.Equal(t, expectedBalance.String(), stateData.Accounts[senderHex].Balance.String())
		assert.Equal(t, transferValue.String(), stateData.Accounts[recipientHex].Balance.String())

		return events
	}

	t.Run("WithoutInvoiceID", func(t *testing.T) {
		events := doTransfer(t, "")

		// Verify invoice_id is absent from both events
		var senderRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[0].Data, &senderRaw))
		assert.NotContains(t, senderRaw, "invoice_id", "invoice_id should be absent from sender event when not provided")

		var recipientRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[1].Data, &recipientRaw))
		assert.NotContains(t, recipientRaw, "invoice_id", "invoice_id should be absent from recipient event when not provided")
	})

	t.Run("WithInvoiceID", func(t *testing.T) {
		invoiceID := "INV-2025-001"
		events := doTransfer(t, invoiceID)

		// Verify invoice_id is present in sender event
		var senderRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[0].Data, &senderRaw))
		assert.Equal(t, invoiceID, senderRaw["invoice_id"], "sender event should contain invoice_id")

		// Verify invoice_id is present in recipient event
		var recipientRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[1].Data, &recipientRaw))
		assert.Equal(t, invoiceID, recipientRaw["invoice_id"], "recipient event should contain invoice_id")
	})
}

func TestIntegration_ProcessRequest_Withdrawal(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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

	newState, events, withdrawals, _, fuel, failure := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, payloadBytes, state, wasmBytes)
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

func TestIntegration_ProcessRequest_Deanonymize(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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
	expectedBalance := new(types.Uint256).SetBytes(depositAmount.Bytes())
	assert.Equal(t, expectedBalance.String(), report.Accounts[sender].Balance.String())
}

func TestIntegration_ProcessRequest_Deanonymize_TxHistory(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	senderHex := fmt.Sprintf("0xadd%037x", 1)
	ethSender := ethCommon.HexToAddress(senderHex)
	recipientHex := fmt.Sprintf("0xadd%037x", 2)
	ethRecipient := ethCommon.HexToAddress(recipientHex)
	withdrawAddrHex := "0x1234567890123456789012345678901234567890"

	depositAmount := big.NewInt(2_000_000_000_000_000_000)
	transferValue := types.NewUint256(500_000_000_000_000_000)
	withdrawValue := types.NewUint256(100_000_000_000_000_000)

	// Load module + deposit + transfer + withdrawal
	state, _, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	state, _, _, failure := runtime.Deposit(ctx, appId, ethSender, depositAmount, state, wasmBytes)
	require.Nil(t, failure)

	recAddress, err := types.HexToAddress(recipientHex)
	require.NoError(t, err)
	transferPayload := app.PayloadInstructions{
		Type:     "transfer",
		Transfer: &app.TransferInstruction{To: recAddress, Amount: transferValue, InvoiceID: "INV-001"},
	}
	transferBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)
	state, _, _, _, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, transferBytes, state, wasmBytes)
	require.Nil(t, failure2)

	withdrawAddr, err := types.HexToAddress(withdrawAddrHex)
	require.NoError(t, err)
	withdrawPayload := app.PayloadInstructions{
		Type:     "withdraw",
		Withdraw: &app.WithdrawInstruction{To: withdrawAddr, Amount: withdrawValue},
	}
	withdrawBytes, err := json.Marshal(withdrawPayload)
	require.NoError(t, err)
	state, _, _, _, _, failure2 = runtime.ProcessRequest(ctx, appId, ethSender, common.Process, withdrawBytes, state, wasmBytes)
	require.Nil(t, failure2)

	// Verify state has transaction records
	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(state, &stateData))
	require.Len(t, stateData.Transactions, 3, "should have deposit + transfer + withdrawal")

	// tx_history report for sender — should see all 3 transactions
	senderAddr, err := types.HexToAddress(senderHex)
	require.NoError(t, err)
	deanonPayload := app.PayloadInstructions{
		Deanonymize: &app.DeanonymizeInstruction{ReportType: "tx_history", Address: senderAddr},
	}
	payloadBytes, err := json.Marshal(deanonPayload)
	require.NoError(t, err)

	_, _, _, reportBytes, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, common.Deanonymize, payloadBytes, state, wasmBytes)
	require.Nil(t, failure2)
	require.NotNil(t, reportBytes)

	var report app.TxHistoryReport
	require.NoError(t, json.Unmarshal(reportBytes, &report))
	assert.Equal(t, senderHex, report.Address.Hex())
	require.Len(t, report.Transactions, 3, "sender involved in all 3 transactions")
	assert.Equal(t, "deposit", report.Transactions[0].Type)
	assert.Equal(t, "transfer", report.Transactions[1].Type)
	assert.Equal(t, "INV-001", report.Transactions[1].InvoiceID)
	assert.Equal(t, "withdrawal", report.Transactions[2].Type)

	// tx_history report for recipient — should only see the transfer
	recipientAddr, err := types.HexToAddress(recipientHex)
	require.NoError(t, err)
	deanonPayload2 := app.PayloadInstructions{
		Deanonymize: &app.DeanonymizeInstruction{ReportType: "tx_history", Address: recipientAddr},
	}
	payloadBytes2, err := json.Marshal(deanonPayload2)
	require.NoError(t, err)

	_, _, _, reportBytes2, _, failure2 := runtime.ProcessRequest(ctx, appId, ethRecipient, common.Deanonymize, payloadBytes2, state, wasmBytes)
	require.Nil(t, failure2)
	require.NotNil(t, reportBytes2)

	var report2 app.TxHistoryReport
	require.NoError(t, json.Unmarshal(reportBytes2, &report2))
	require.Len(t, report2.Transactions, 1, "recipient only involved in transfer")
	assert.Equal(t, "transfer", report2.Transactions[0].Type)

	// Backward compatibility: empty payload defaults to balances
	_, _, _, balanceReportBytes, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, common.Deanonymize, []byte("{}"), state, wasmBytes)
	require.Nil(t, failure2)
	require.NotNil(t, balanceReportBytes)

	var balanceReport app.DeanonymizationReport
	require.NoError(t, json.Unmarshal(balanceReportBytes, &balanceReport))
	require.Contains(t, balanceReport.Accounts, senderHex)

	// Error: tx_history without address should fail
	badPayload := app.PayloadInstructions{
		Deanonymize: &app.DeanonymizeInstruction{ReportType: "tx_history"},
	}
	badBytes, err := json.Marshal(badPayload)
	require.NoError(t, err)
	_, _, _, _, _, failure2 = runtime.ProcessRequest(ctx, appId, ethSender, common.Deanonymize, badBytes, state, wasmBytes)
	require.NotNil(t, failure2, "tx_history without address should fail")
}

func TestIntegration_ProcessRequest_Deanonymize_TxHistory_TimestampFilter(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	senderHex := fmt.Sprintf("0xadd%037x", 1)
	ethSender := ethCommon.HexToAddress(senderHex)
	recipientHex := fmt.Sprintf("0xadd%037x", 2)

	depositAmount := big.NewInt(2_000_000_000_000_000_000)
	transferValue := types.NewUint256(500_000_000_000_000_000)
	withdrawValue := types.NewUint256(100_000_000_000_000_000)

	// Build state with 3 transactions: deposit, transfer, withdrawal
	state, _, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	state, _, _, failure := runtime.Deposit(ctx, appId, ethSender, depositAmount, state, wasmBytes)
	require.Nil(t, failure)

	recAddress, err := types.HexToAddress(recipientHex)
	require.NoError(t, err)
	transferPayload := app.PayloadInstructions{
		Type:     "transfer",
		Transfer: &app.TransferInstruction{To: recAddress, Amount: transferValue},
	}
	transferBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)
	state, _, _, _, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, transferBytes, state, wasmBytes)
	require.Nil(t, failure2)

	withdrawAddr, err := types.HexToAddress("0x1234567890123456789012345678901234567890")
	require.NoError(t, err)
	withdrawPayload := app.PayloadInstructions{
		Type:     "withdraw",
		Withdraw: &app.WithdrawInstruction{To: withdrawAddr, Amount: withdrawValue},
	}
	withdrawBytes, err := json.Marshal(withdrawPayload)
	require.NoError(t, err)
	state, _, _, _, _, failure2 = runtime.ProcessRequest(ctx, appId, ethSender, common.Process, withdrawBytes, state, wasmBytes)
	require.Nil(t, failure2)

	// Verify timestamps are populated (non-zero)
	var stateData app.ApplicationInternalState
	require.NoError(t, json.Unmarshal(state, &stateData))
	require.Len(t, stateData.Transactions, 3)
	for i, tx := range stateData.Transactions {
		assert.Greater(t, tx.Timestamp, int64(0), "transaction %d should have a non-zero timestamp", i)
	}

	// Override timestamps with known values for deterministic filtering
	// deposit=1000, transfer=2000, withdrawal=3000
	stateData.Transactions[0].Timestamp = 1000
	stateData.Transactions[1].Timestamp = 2000
	stateData.Transactions[2].Timestamp = 3000
	state, err = json.Marshal(stateData)
	require.NoError(t, err)

	senderAddr, err := types.HexToAddress(senderHex)
	require.NoError(t, err)

	// Helper to request tx_history with optional timestamp range
	requestTxHistory := func(t *testing.T, fromTs, toTs int64) app.TxHistoryReport {
		t.Helper()
		payload := app.PayloadInstructions{
			Deanonymize: &app.DeanonymizeInstruction{
				ReportType:    "tx_history",
				Address:       senderAddr,
				FromTimestamp: fromTs,
				ToTimestamp:   toTs,
			},
		}
		payloadBytes, err := json.Marshal(payload)
		require.NoError(t, err)
		_, _, _, reportBytes, _, fail := runtime.ProcessRequest(ctx, appId, ethSender, common.Deanonymize, payloadBytes, state, wasmBytes)
		require.Nil(t, fail)
		require.NotNil(t, reportBytes)
		var report app.TxHistoryReport
		require.NoError(t, json.Unmarshal(reportBytes, &report))
		return report
	}

	t.Run("NoFilter_ReturnsAll", func(t *testing.T) {
		report := requestTxHistory(t, 0, 0)
		require.Len(t, report.Transactions, 3)
		assert.NotNil(t, report.Balance, "report should include balance")
	})

	t.Run("FromTimestamp_FilterOldest", func(t *testing.T) {
		// from=1500 should exclude deposit(1000), keep transfer(2000)+withdrawal(3000)
		report := requestTxHistory(t, 1500, 0)
		require.Len(t, report.Transactions, 2)
		assert.Equal(t, "transfer", report.Transactions[0].Type)
		assert.Equal(t, "withdrawal", report.Transactions[1].Type)
	})

	t.Run("ToTimestamp_FilterNewest", func(t *testing.T) {
		// to=2500 should exclude withdrawal(3000), keep deposit(1000)+transfer(2000)
		report := requestTxHistory(t, 0, 2500)
		require.Len(t, report.Transactions, 2)
		assert.Equal(t, "deposit", report.Transactions[0].Type)
		assert.Equal(t, "transfer", report.Transactions[1].Type)
	})

	t.Run("BothTimestamps_Range", func(t *testing.T) {
		// from=1500, to=2500 should keep only transfer(2000)
		report := requestTxHistory(t, 1500, 2500)
		require.Len(t, report.Transactions, 1)
		assert.Equal(t, "transfer", report.Transactions[0].Type)
	})

	t.Run("NoMatch_EmptyArray", func(t *testing.T) {
		// from=5000, to=6000 should match nothing
		report := requestTxHistory(t, 5000, 6000)
		require.Len(t, report.Transactions, 0)

		// Verify JSON has [] not null
		payloadInstr := app.PayloadInstructions{
			Deanonymize: &app.DeanonymizeInstruction{
				ReportType:    "tx_history",
				Address:       senderAddr,
				FromTimestamp: 5000,
				ToTimestamp:   6000,
			},
		}
		payloadBytes, err := json.Marshal(payloadInstr)
		require.NoError(t, err)
		_, _, _, reportBytes, _, fail := runtime.ProcessRequest(ctx, appId, ethSender, common.Deanonymize, payloadBytes, state, wasmBytes)
		require.Nil(t, fail)
		assert.Contains(t, string(reportBytes), `"transactions":[]`, "empty transactions should be [] not null")
	})

	t.Run("BalanceIncluded", func(t *testing.T) {
		report := requestTxHistory(t, 0, 0)
		require.NotNil(t, report.Balance)
		// sender deposited 2 ETH, transferred 0.5 ETH, withdrew 0.1 ETH => 1.4 ETH remaining
		expectedBalance := types.NewUint256(1_400_000_000_000_000_000)
		assert.Equal(t, expectedBalance.String(), report.Balance.String())
	})
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
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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

	state, _, _, _, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, transferBytes, state, wasmBytes)
	require.Nil(t, failure2)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after ProcessRequest (transfer)")

	// Withdraw
	withdrawPayload := app.PayloadInstructions{
		Type:     "withdraw",
		Withdraw: &app.WithdrawInstruction{To: withdrawAddress, Amount: types.NewUint256(50)},
	}
	withdrawBytes, err := json.Marshal(withdrawPayload)
	require.NoError(t, err)

	stateBytes, _, _, _, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, withdrawBytes, state, wasmBytes)
	require.Nil(t, failure2)
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after ProcessRequest (withdraw)")

	deanonPayload := app.PayloadInstructions{
		Type:        "deanonymize",
		Deanonymize: &app.DeanonymizeInstruction{},
	}
	payloadBytes, err := json.Marshal(deanonPayload)
	require.NoError(t, err)

	stateBytes, _, _, _, _, failure = runtime.ProcessRequest(ctx, appId, ethSender, common.Deanonymize, payloadBytes, stateBytes, wasmBytes)

	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after GenerateDeanonymizationReport")
}

// TestIntegration_ErrorPathMemory verifies that error results returned by the guest
// (which still use SerializeAndWriteResult → BytesToPtr) do not leak memory.
func TestIntegration_ErrorPathMemory(t *testing.T) {
	wasmBytes := readWasm(t)
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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

	_, _, _, _, _, failure2 := runtime.ProcessRequest(ctx, appId, ethSender, common.Process, payloadBytes, state, wasmBytes)
	require.NotNil(t, failure2, "expected error for insufficient balance")
	requireMemoryClean(t, runtime, appId, wasmBytes, "memory leak after insufficient balance error")

	// Error: transfer from non-existent account
	transferPayload := app.PayloadInstructions{
		Type:     "transfer",
		Transfer: &app.TransferInstruction{To: withdrawAddress, Amount: types.NewUint256(1)},
	}
	transferBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)

	_, _, _, _, _, failure2 = runtime.ProcessRequest(ctx, appId, nonExistentUser, common.Process, transferBytes, state, wasmBytes)
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
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 1)
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

	deanonPayload := app.PayloadInstructions{
		Type:        "deanonymize",
		Deanonymize: &app.DeanonymizeInstruction{},
	}
	payloadBytes, err := json.Marshal(deanonPayload)
	require.NoError(t, err)

	_, _, _, reportBytes, _, failure := runtime.ProcessRequest(ctx, appId, ethCommon.Address{}, common.Deanonymize, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	require.NotNil(t, reportBytes)

	// Verify report contains all accounts
	var report app.DeanonymizationReport
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
