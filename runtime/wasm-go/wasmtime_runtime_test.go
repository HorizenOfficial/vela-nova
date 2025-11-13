package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/horizen-pes/pkg/common"
	wasm "github.com/horizen-pes/pkg/wasm"
	wasmCommon "github.com/horizen-pes/pkg/wasm/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Local helper types to avoid importing the app submodule
// They mirror the JSON shapes expected by the WASM app.
type PayloadInstructions struct {
	Type     string               `json:"type"`
	Transfer *TransferInstruction `json:"transfer,omitempty"`
	Withdraw *WithdrawInstruction `json:"withdraw,omitempty"`
}

type TransferInstruction struct {
	To     ethCommon.Address `json:"to"`
	Amount *big.Int            `json:"amount"`
}

type WithdrawInstruction struct {
	To     ethCommon.Address `json:"to"`
	Amount *big.Int            `json:"amount"`
}

type AppState struct {
	AppID    common.ApplicationIdType `json:"appId"`
	Accounts map[ethCommon.Address] struct {
		Balance *big.Int `json:"balance"`
	} `json:"accounts"`
	Nonce uint64 `json:"nonce"`
}

func TestWasmtimeRuntime_LoadModule(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	// Create runtime
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	// Test LoadModule
	ctx := context.Background()
	appId := common.NewApplicationId(1)

	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.NotNil(t, state, "State should not be nil")

	// Verify the state is valid JSON
	var stateData AppState
	err = json.Unmarshal(state, &stateData)
	require.NoError(t, err, "State should be valid JSON")

	// Check that the state contains expected fields
	assert.Equal(t, appId, stateData.AppID)
	assert.Equal(t, 0, len(stateData.Accounts))
	assert.True(t, stateData.Nonce == 0)
}

func TestWasmtimeRuntime_Deposit(t *testing.T) {

	type TestAccountState struct {
		Balance *big.Int `json:"balance"`
	}

	type TestStateData struct {
		AppId    common.ApplicationIdType               `json:"appId"`
		Accounts map[ethCommon.Address]TestAccountState `json:"accounts"`
		Nonce    uint64                                 `json:"nonce"`
	}

	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	// Create runtime
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	// Load module first
	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender :=  ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	value := big.NewInt(1000000000000000000) // 1 ETH

	initialState, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")

	// Test Deposit
	newState, events, err := runtime.Deposit(ctx, appId, sender, value, initialState, wasmBytes)
	require.NoError(t, err, "Deposit should succeed")
	require.NotNil(t, newState, "New state should not be nil")
	require.Len(t, events, 1, "Should generate one event")

	// Verify the event
	event := events[0]
	assert.Equal(t, sender, event.UserID)

	var eventData wasmCommon.DepositEvent
	err = json.Unmarshal(event.Data, &eventData)
	require.NoError(t, err, "Event data should be valid JSON")
	assert.Equal(t, "deposit", eventData.Type)
	assert.Equal(t, value, eventData.Amount)

	// Verify the state was updated
	var stateData TestStateData
	err = json.Unmarshal(newState, &stateData)
	require.NoError(t, err, "New state should be valid JSON")

	require.Contains(t, stateData.Accounts, sender)
	assert.Equal(t, value, stateData.Accounts[sender].Balance)
}

func TestWasmtimeRuntime_ProcessRequest_Transfer(t *testing.T) {

	type TestAccountState struct {
		Balance *big.Int `json:"balance"`
	}

	type TestStateData struct {
		AppId    common.ApplicationIdType              `json:"appId"`
		Accounts map[ethCommon.Address]TestAccountState `json:"accounts"`
		Nonce    uint64                      `json:"nonce"`
	}

	type TestTransferEventData struct {
		Type   string `json:"type"`
		From   ethCommon.Address `json:"from,omitempty"`
		To     ethCommon.Address `json:"to,omitempty"`
		Amount *big.Int `json:"amount"`
	}

	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	// Create runtime
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	recipient := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))
	depositValue := big.NewInt(2000000000000000000) // 2 ETH
	transferValue := big.NewInt(500000000000000000) // 0.5 ETH

	// Load module and make a deposit first
	initialState, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")

	stateAfterDeposit, _, err := runtime.Deposit(ctx, appId, sender, depositValue, initialState, wasmBytes)
	require.NoError(t, err, "Deposit should succeed")

	// Create transfer payload
	transferPayload := PayloadInstructions{
		Type: "transfer",
		Transfer: &TransferInstruction{
			To:     recipient,
			Amount: transferValue,
		},
	}
	payloadBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err, "Should marshal transfer payload")

	// Test ProcessRequest for transfer
	newState, events, withdrawals, err := runtime.ProcessRequest(ctx, appId, sender, payloadBytes, stateAfterDeposit, wasmBytes)
	require.NoError(t, err, "ProcessRequest should succeed")
	require.NotNil(t, newState, "New state should not be nil")
	require.Len(t, events, 2, "Should generate two events (sender and recipient)")
	require.Len(t, withdrawals, 0, "Should not generate withdrawals")

	// Verify sender event
	var senderEventData TestTransferEventData
	err = json.Unmarshal(events[0].Data, &senderEventData)
	require.NoError(t, err)
	assert.Equal(t, "transfer_sent", senderEventData.Type)
	assert.Equal(t, recipient, senderEventData.To)
	assert.Equal(t, transferValue, senderEventData.Amount)

	// Verify recipient event
	var recipientEventData TestTransferEventData
	err = json.Unmarshal(events[1].Data, &recipientEventData)
	require.NoError(t, err)
	assert.Equal(t, "transfer_received", recipientEventData.Type)
	assert.Equal(t, sender, recipientEventData.From)
	assert.Equal(t, transferValue, recipientEventData.Amount)

	// Verify the state was updated
	var stateData TestStateData
	err = json.Unmarshal(newState, &stateData)
	require.NoError(t, err, "New state should be valid JSON")

	updatedBalanceSender := new(big.Int).Sub(depositValue, transferValue)
	assert.Equal(t, updatedBalanceSender, stateData.Accounts[sender].Balance)
	assert.Equal(t, transferValue, stateData.Accounts[recipient].Balance)
}

func TestWasmtimeRuntime_ProcessRequest_Withdrawal(t *testing.T) {

	type TestAccountState struct {
		Balance *big.Int `json:"balance"`
	}

	type TestStateData struct {
		AppId    common.ApplicationIdType              `json:"appId"`
		Accounts map[ethCommon.Address]TestAccountState `json:"accounts"`
		Nonce    uint64                      `json:"nonce"`
	}

	type TestWithdrawalEventData struct {
		Type   string `json:"type"`
		To     ethCommon.Address `json:"to"`
		Amount *big.Int `json:"amount"`
	}

	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	// Create runtime
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	depositValue := big.NewInt(1000000000000000000) // 1 ETH
	withdrawValue := big.NewInt(500000000000000000) // 0.5 ETH
	withdrawAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")

	// Load module and make a deposit first
	initialState, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")

	stateAfterDeposit, _, err := runtime.Deposit(ctx, appId, sender, depositValue, initialState, wasmBytes)
	require.NoError(t, err, "Deposit should succeed")

	// Create withdrawal payload
	withdrawPayload := PayloadInstructions{
		Type: "withdraw",
		Withdraw: &WithdrawInstruction{
			To:     withdrawAddress,
			Amount: withdrawValue,
		},
	}
	payloadBytes, err := json.Marshal(withdrawPayload)
	require.NoError(t, err, "Should marshal withdrawal payload")

	// Test ProcessRequest for withdrawal
	newState, events, withdrawals, err := runtime.ProcessRequest(ctx, appId, sender, payloadBytes, stateAfterDeposit, wasmBytes)
	require.NoError(t, err, "ProcessRequest should succeed")
	require.NotNil(t, newState, "New state should not be nil")
	require.Len(t, events, 1, "Should generate one event")
	require.Len(t, withdrawals, 1, "Should generate one withdrawal")

	// Verify withdrawal event
	event := events[0]
	assert.Equal(t, sender, event.UserID)

	var eventData TestWithdrawalEventData
	err = json.Unmarshal(event.Data, &eventData)
	require.NoError(t, err, "Event data should be valid JSON")
	assert.Equal(t, "withdrawal", eventData.Type)
	assert.Equal(t, withdrawAddress, eventData.To)
	assert.Equal(t, withdrawValue, eventData.Amount)

	// Verify withdrawal
	withdrawal := withdrawals[0]
	assert.Equal(t, withdrawAddress, withdrawal.DestinationAddress)
	assert.Equal(t, withdrawValue, withdrawal.Amount)

	// Verify the state was updated
	var stateData TestStateData
	err = json.Unmarshal(newState, &stateData)
	require.NoError(t, err, "New state should be valid JSON")
	updatedBalance := new(big.Int).Sub(depositValue, withdrawValue)

	assert.Equal(t, updatedBalance, stateData.Accounts[sender].Balance)
}

func TestWasmtimeRuntime_GenerateDeanonymizationReport(t *testing.T) {

	type TestAccountState struct {
		Balance *big.Int `json:"balance"`
	}

	type TestDeanonymizationReport struct {
		ApplicationId common.ApplicationIdType                      `json:"applicationId"`
		RequestId     common.RequestIdType                      `json:"requestId"`
		Accounts      map[ethCommon.Address]TestAccountState `json:"accounts"`
		Nonce         uint64                      `json:"nonce"`
	}

	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	// Create runtime
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	value := big.NewInt(1000000000000000000) // 1 ETH

	// Load module and make a deposit first to have some state
	initialState, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")

	stateWithData, _, err := runtime.Deposit(ctx, appId, sender, value, initialState, wasmBytes)
	require.NoError(t, err, "Deposit should succeed")

	// Test GenerateDeanonymizationReport
	report, err := runtime.GenerateDeanonymizationReport(ctx, appId, []byte("{}"), stateWithData, wasmBytes)
	require.NoError(t, err, "GenerateDeanonymizationReport should succeed")
	require.NotNil(t, report, "Report should not be nil")

	var reportData TestDeanonymizationReport
	err = json.Unmarshal(report, &reportData)
	require.NoError(t, err, "Report should be valid JSON")

	require.Contains(t, reportData.Accounts, sender)
	assert.Equal(t, value, reportData.Accounts[sender].Balance)
}

func TestWasmtimeRuntime_FullWorkflow(t *testing.T) {

	type TestAccountState struct {
		Balance *big.Int `json:"balance"`
	}

	type TestStateData struct {
		AppId    common.ApplicationIdType                      `json:"appId"`
		Accounts map[ethCommon.Address]TestAccountState `json:"accounts"`
		Nonce    uint64                      `json:"nonce"`
	}

	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	// Create runtime
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	user2 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))

	t.Log("Step 1: Load module")
	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.NotNil(t, state)

	t.Log("Step 2: Make deposit for user1")
	depositValue := big.NewInt(2000000000000000000) // 2 ETH
	state, events, err := runtime.Deposit(ctx, appId, user1, depositValue, state, wasmBytes)
	require.NoError(t, err, "Deposit should succeed")
	require.Len(t, events, 1)

	t.Log("Step 3: Transfer from user1 to user2")
	transferValue := big.NewInt(500000000000000000) // 0.5 ETH
	transferPayload := PayloadInstructions{
		Type: "transfer",
		Transfer: &TransferInstruction{
			To:     user2,
			Amount: transferValue,
		},
	}
	payloadBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)

	state, events, withdrawals, err := runtime.ProcessRequest(ctx, appId, user1, payloadBytes, state, wasmBytes)
	require.NoError(t, err, "Transfer should succeed")
	require.Len(t, events, 2)
	require.Len(t, withdrawals, 0)

	t.Log("Step 4: Withdraw from user2")
	// Create withdrawal payload
	withdrawValue := big.NewInt(250000000000000000) // 0.25 ETH
	withdrawAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")
	withdrawPayload := PayloadInstructions{
		Type: "withdraw",
		Withdraw: &WithdrawInstruction{
			To:     withdrawAddress,
			Amount: withdrawValue,
		},
	}
	payloadBytes, err = json.Marshal(withdrawPayload)
	require.NoError(t, err)

	state, events, withdrawals, err = runtime.ProcessRequest(ctx, appId, user2, payloadBytes, state, wasmBytes)
	require.NoError(t, err, "Withdrawal should succeed")
	require.Len(t, events, 1)
	require.Len(t, withdrawals, 1)

	t.Log("Step 5: Generate deanonymization report")
	report, err := runtime.GenerateDeanonymizationReport(ctx, appId, []byte("{}"), state, wasmBytes)
	require.NoError(t, err, "Deanonymization report should succeed")
	require.NotNil(t, report)

	var stateData TestStateData
	err = json.Unmarshal(state, &stateData)
	require.NoError(t, err)

	user1UpdatedBalance := new(big.Int).Sub(depositValue, transferValue)
	user2UpdatedBalance := new(big.Int).Sub(transferValue, withdrawValue)
	assert.Equal(t, user1UpdatedBalance, stateData.Accounts[user1].Balance)
	assert.Equal(t, user2UpdatedBalance, stateData.Accounts[user2].Balance)

	t.Log("Full workflow completed successfully!")
}

func TestWasmtimeRuntime_ConcurrentModuleLoading(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 3)

	// Load the same module concurrently
	for i := range 3 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			appId := common.NewApplicationId(int64(id))
			_, err := runtime.LoadModule(ctx, appId, wasmBytes)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		require.NoError(t, err, "Concurrent module loading should succeed")
	}
}

func TestWasmtimeRuntime_LargeStateHandling(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	// Load module and make many deposits to create large state
	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	// Make 100 deposits to create a large state
	// Tested up to 6k, after 6k app is very slow and failing randomly: TODO check this!
	for i := range 100 {
		user := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", i))
		value := big.NewInt(1000000000000000000) // 1 ETH
		state, _, err = runtime.Deposit(ctx, appId, user, value, state, wasmBytes)
		require.NoError(t, err)
	}

	// Verify the large state can still be processed
	transferPayload := PayloadInstructions{
		Type: "transfer",
		Transfer: &TransferInstruction{
			To:     ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1)),
			Amount: big.NewInt(500000000000000000),
		},
	}
	payloadBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)

	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 0))
	_, events, withdrawals, err := runtime.ProcessRequest(ctx, appId, sender, payloadBytes, state, wasmBytes)
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Len(t, withdrawals, 0)
}

func TestWasmtimeRuntime_InvalidWasmModule(t *testing.T) {
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	invalidWasm := []byte("invalid wasm bytes")

	_, err := runtime.LoadModule(ctx, appId, invalidWasm)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile WASM module")
}

func TestWasmtimeRuntime_EmptyWasmModule(t *testing.T) {
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	emptyWasm := []byte{}

	_, err := runtime.LoadModule(ctx, appId, emptyWasm)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile WASM module")
}

func TestWasmtimeRuntime_NilInputs(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	appId := common.NewApplicationId(1)

	t.Run("NilWasmBytes", func(t *testing.T) {
		_, err := runtime.LoadModule(ctx, appId, nil)
		assert.Error(t, err)
	})

	t.Run("InvalidAppId", func(t *testing.T) {
		appId := common.ApplicationIdType(math.MaxInt64 + 1) // Invalid app ID
		_, err := runtime.LoadModule(ctx, appId, wasmBytes)
		// This might succeed depending on implementation, but state should be testable
		if err == nil {
			// Verify we can't use invalid app ID for operations
			_, _, err = runtime.Deposit(ctx, appId, user1, big.NewInt(1000), []byte("{}"), wasmBytes)
			assert.Error(t, err)
		}
	})

	t.Run("NilState", func(t *testing.T) {
		appId := common.NewApplicationId(1)
		_, _, err := runtime.Deposit(ctx, appId, user1, big.NewInt(1000), nil, wasmBytes)
		assert.Error(t, err)
	})
}

func TestWasmtimeRuntime_InvalidPayloads(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	user2 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))

	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	t.Run("InvalidJSON", func(t *testing.T) {
		invalidPayload := []byte("invalid json")
		_, _, _, err := runtime.ProcessRequest(ctx, appId, user1, invalidPayload, state, wasmBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to parse payload instructions")
	})

	t.Run("MissingTransferFields", func(t *testing.T) {
		incompletePayload := map[string]interface{}{
			"type": "transfer",
			// Missing transfer field
		}
		payloadBytes, _ := json.Marshal(incompletePayload)
		_, _, _, err := runtime.ProcessRequest(ctx, appId, user1, payloadBytes, state, wasmBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Transfer instruction is missing")
	})

	t.Run("AccountDoesNotExist", func(t *testing.T) {
		negativePayload := map[string]interface{}{
			"type": "transfer",
			"transfer": map[string]interface{}{
				"to":     user2,
				"amount": uint64(500),
			},
		}
		payloadBytes, _ := json.Marshal(negativePayload)
		_, _, _, err := runtime.ProcessRequest(ctx, appId, user1, payloadBytes, state, wasmBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Account does not exist")
	})
}

func TestWasmtimeRuntime_InsufficientFunds(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	user2 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))
	value := new (big.Int).SetUint64(12345678901234567890) // # fits in uint64, > max int64

	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	deposit := new(big.Int).Div(value, big.NewInt(2))
	state, _, err = runtime.Deposit(ctx, appId, user1, deposit, state, wasmBytes)
	require.NoError(t, err)

	// Try to transfer without enough funds
	transferPayload := PayloadInstructions{
		Type: "transfer",
		Transfer: &TransferInstruction{
			To:     user2,
			Amount: value,
		},
	}
	payloadBytes, _ := json.Marshal(transferPayload)

	_, _, _, err = runtime.ProcessRequest(ctx, appId, user1, payloadBytes, state, wasmBytes)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Insufficient balance for transfer")
}

func TestWasmtimeRuntime_LargePayload(t *testing.T) {
	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	// Create an extremely large payload
	largePayload := make([]byte, 10*1024*1024) // 10MB
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	wasmPath := filepath.Join("build", "payment_app.wasm")
	wasmBytes, err := os.ReadFile(wasmPath)
	require.NoError(t, err)

	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	_, _, _, err = runtime.ProcessRequest(ctx, appId, user1, largePayload, state, wasmBytes)
	// should not panic but return an error that the payload does not conform to the expected format
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to parse payload instructions")
}

func TestWasmtimeRuntime_InvalidStateFormat(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))

	// Use corrupted state
	corruptedState := []byte("corrupted state data")

	state, events, err := runtime.Deposit(ctx, appId, user1, big.NewInt(1000), corruptedState, wasmBytes)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Failed to parse application state")
	require.Equal(t, []byte(nil), state)
	require.Equal(t, []common.PlainEvent(nil), events)
}

func TestWasmtimeRuntime_MultipleLoadModule(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()

	// Load same module multiple times (TODO this will change)
	for i := 0; i < 5; i++ {
		_, err := runtime.LoadModule(ctx, common.NewApplicationId(int64(i)), wasmBytes)
		require.NoError(t, err)
	}
}

func TestWasmtimeRuntime_ZeroValueOperations(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))

	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	// Test zero value deposit
	newState, events, err := runtime.Deposit(ctx, appId, user1, big.NewInt(0), state, wasmBytes)

	require.NoError(t, err, "Deposit with zero value should succeed")
	require.Len(t, events, 0, "Zero value deposit should not generate any events")
	require.NotNil(t, state, "State should not be nil after zero value deposit")
	require.Equal(t, state, newState)
}

func TestWasmtimeRuntime_InvalidInstruction(t *testing.T) {
	// Build and load the compiled WASM module
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime()
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	state, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)

	invalidPayload := map[string]interface{}{
		"type": "unsupported_instruction",
	}
	payloadBytes, err := json.Marshal(invalidPayload)
	require.NoError(t, err)

	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	_, _, _, err = runtime.ProcessRequest(ctx, appId, user1, payloadBytes, state, wasmBytes)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported instruction type")
}
