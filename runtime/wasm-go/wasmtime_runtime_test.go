package main_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/HorizenOfficial/vela/pkg/common"
	wasm "github.com/HorizenOfficial/vela/pkg/wasm"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/sha3"
)

// host-side helper types for test validation (app-specific, not framework types).
// They mirror the JSON shapes expected by the WASM app.
type depositEvent struct {
	Type         string            `json:"type"`
	TokenAddress ethCommon.Address `json:"tokenAddress"`
	Amount       *common.Big       `json:"amount"`
	Balance      *common.Big       `json:"balance"`
	Nonce        uint64            `json:"nonce"`
}

type payloadInstructions struct {
	Type     string               `json:"type"`
	Transfer *transferInstruction `json:"transfer,omitempty"`
	Withdraw *withdrawInstruction `json:"withdraw,omitempty"`
}

type transferInstruction struct {
	To           ethCommon.Address `json:"to"`
	TokenAddress ethCommon.Address `json:"tokenAddress,omitempty"`
	Amount       *common.Big       `json:"amount"`
	InvoiceID    string            `json:"invoice_id,omitempty"`
}

type withdrawInstruction struct {
	To           ethCommon.Address `json:"to"`
	TokenAddress ethCommon.Address `json:"tokenAddress,omitempty"`
	Amount       *common.Big       `json:"amount"`
}

type testAccountState struct {
	Address  ethCommon.Address                `json:"address"`
	Balances map[string]*common.Big           `json:"balances"`
}

type applicationInternalState struct {
	AppID         common.ApplicationIdType              `json:"appId"`
	Accounts      map[ethCommon.Address]testAccountState `json:"accounts"`
	AllowedTokens map[string]bool                       `json:"allowedTokens"`
	Nonce         uint64                                `json:"nonce"`
}

// getTestBalance extracts a balance from the test state data for a given account and token hex.
func getTestBalance(stateData applicationInternalState, account ethCommon.Address, tokenHex string) *big.Int {
	acc, ok := stateData.Accounts[account]
	if !ok || acc.Balances == nil {
		return big.NewInt(0)
	}
	bal, ok := acc.Balances[tokenHex]
	if !ok || bal == nil {
		return big.NewInt(0)
	}
	return bal.ToInt()
}

func TestWasmtimeRuntime_LoadModule(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.NotNil(t, state, "State should not be nil")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	var stateData applicationInternalState
	err = json.Unmarshal(state, &stateData)
	require.NoError(t, err, "State should be valid JSON")

	assert.Equal(t, appId, stateData.AppID)
	assert.Equal(t, 0, len(stateData.Accounts))
	assert.True(t, stateData.Nonce == 0)
	assert.True(t, stateData.AllowedTokens[ethAddressHex()], "ETH should be allowed by default")
}

func TestWasmtimeRuntime_Deposit(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	depositAmount := big.NewInt(1000000000000000000) // 1 ETH

	initialState, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	newState, events, _, fuel, failure := runtime.Deposit(ctx, appId, sender, ethToken, depositAmount, initialState, wasmBytes)
	require.Nil(t, failure, "Deposit should succeed")
	require.NotNil(t, newState, "New state should not be nil")
	require.Len(t, events, 1, "Should generate one event")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	// Verify the event. The WASM app intentionally leaves EventSubType unset
	// because the executor overrides it from the user seed; at this runtime
	// level (no executor) the field is the zero value.
	event := events[0]
	assert.Equal(t, sender, event.UserID)
	assert.Equal(t, [32]byte{}, event.EventSubType)

	var eventData depositEvent
	err = json.Unmarshal(event.Data, &eventData)
	require.NoError(t, err, "Event data should be valid JSON")
	assert.Equal(t, "deposit", eventData.Type)
	assert.Equal(t, depositAmount, eventData.Amount.ToInt())

	// Verify the state was updated
	var stateData applicationInternalState
	err = json.Unmarshal(newState, &stateData)
	require.NoError(t, err, "New state should be valid JSON")

	require.Contains(t, stateData.Accounts, sender)
	assert.Equal(t, depositAmount, getTestBalance(stateData, sender, ethAddressHex()))
}

func TestWasmtimeRuntime_ProcessRequest_Transfer(t *testing.T) {
	type TestTransferEventData struct {
		Type         string            `json:"type"`
		From         ethCommon.Address `json:"from,omitempty"`
		To           ethCommon.Address `json:"to,omitempty"`
		TokenAddress ethCommon.Address `json:"tokenAddress"`
		Amount       *common.Big       `json:"amount"`
		InvoiceID    string            `json:"invoice_id,omitempty"`
	}

	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	recipient := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))
	depositAmount := big.NewInt(2000000000000000000) // 2 ETH
	transferValue := big.NewInt(500000000000000000)  // 0.5 ETH

	initialState, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	stateAfterDeposit, _, _, fuel, failure := runtime.Deposit(ctx, appId, sender, ethToken, depositAmount, initialState, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	// helper: build payload, execute transfer, verify state, return events
	doTransfer := func(t *testing.T, invoiceID string) ([]common.PlainEvent, []common.AppEvent) {
		t.Helper()
		ti := &transferInstruction{
			To:        recipient,
			Amount:    common.ToBig(transferValue),
			InvoiceID: invoiceID,
		}
		payloadBytes, err := json.Marshal(payloadInstructions{Type: "transfer", Transfer: ti})
		require.NoError(t, err)

		newState, events, appEvents, withdrawals, reportBytes, fuel, failure := runtime.ProcessRequest(
			ctx, appId, sender, common.Process, payloadBytes, stateAfterDeposit, wasmBytes)
		require.Nil(t, failure)
		require.NotNil(t, newState, "New state should not be nil")
		require.Len(t, events, 2, "Should generate two events (sender and recipient)")
		require.Len(t, withdrawals, 0, "Should not generate withdrawals")
		require.Nil(t, reportBytes, "Report should be nil for transfer requests")
		require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

		// Verify the state was updated
		var stateData applicationInternalState
		require.NoError(t, json.Unmarshal(newState, &stateData))
		updatedBalanceSender := new(big.Int).Sub(depositAmount, transferValue)
		assert.Equal(t, updatedBalanceSender, getTestBalance(stateData, sender, ethAddressHex()))
		assert.Equal(t, transferValue, getTestBalance(stateData, recipient, ethAddressHex()))

		return events, appEvents
	}

	t.Run("WithoutInvoiceID", func(t *testing.T) {
		events, appEvents := doTransfer(t, "")

		// Verify sender event
		var senderEventData TestTransferEventData
		require.NoError(t, json.Unmarshal(events[0].Data, &senderEventData))
		assert.Equal(t, "transfer_sent", senderEventData.Type)
		assert.Equal(t, recipient, senderEventData.To)
		assert.Equal(t, transferValue, senderEventData.Amount.ToInt())

		// Verify recipient event
		var recipientEventData TestTransferEventData
		require.NoError(t, json.Unmarshal(events[1].Data, &recipientEventData))
		assert.Equal(t, "transfer_received", recipientEventData.Type)
		assert.Equal(t, sender, recipientEventData.From)
		assert.Equal(t, transferValue, recipientEventData.Amount.ToInt())

		// Verify invoice_id is absent from both events
		var senderRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[0].Data, &senderRaw))
		assert.NotContains(t, senderRaw, "invoice_id", "invoice_id should be absent from sender event when not provided")

		var recipientRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[1].Data, &recipientRaw))
		assert.NotContains(t, recipientRaw, "invoice_id", "invoice_id should be absent from recipient event when not provided")

		// No AppEvent when InvoiceID is empty
		assert.Empty(t, appEvents, "no AppEvent should be emitted without InvoiceID")
	})

	t.Run("WithInvoiceID", func(t *testing.T) {
		invoiceID := "INV-2025-001"
		events, appEvents := doTransfer(t, invoiceID)

		// Verify sender event contains invoice_id
		var senderEventData TestTransferEventData
		require.NoError(t, json.Unmarshal(events[0].Data, &senderEventData))
		assert.Equal(t, "transfer_sent", senderEventData.Type)
		assert.Equal(t, invoiceID, senderEventData.InvoiceID)

		// Verify recipient event contains invoice_id
		var recipientEventData TestTransferEventData
		require.NoError(t, json.Unmarshal(events[1].Data, &recipientEventData))
		assert.Equal(t, "transfer_received", recipientEventData.Type)
		assert.Equal(t, invoiceID, recipientEventData.InvoiceID)

		// Verify invoice_id is present in raw JSON
		var senderRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[0].Data, &senderRaw))
		assert.Equal(t, invoiceID, senderRaw["invoice_id"])

		var recipientRaw map[string]interface{}
		require.NoError(t, json.Unmarshal(events[1].Data, &recipientRaw))
		assert.Equal(t, invoiceID, recipientRaw["invoice_id"])

		// AppEvent should be emitted with the receipt hash carried in EventSubType and Data left nil.
		require.Len(t, appEvents, 1, "one AppEvent should be emitted when InvoiceID is present")

		// Verify the hash matches keccak256(len32(invoiceID) || invoiceID || sender || tokenAddress || amount || to).
		// The uint32 big-endian length prefix and the fixed 32-byte amount encoding
		// ensure field boundaries are unambiguous (prevents cross-transfer collisions).
		invoiceIDBytes := []byte(invoiceID)
		var lenPrefix [4]byte
		binary.BigEndian.PutUint32(lenPrefix[:], uint32(len(invoiceIDBytes)))

		var amountFixed [32]byte
		transferValue.FillBytes(amountFixed[:])

		h := sha3.NewLegacyKeccak256()
		h.Write(lenPrefix[:])
		h.Write(invoiceIDBytes)
		h.Write(sender.Bytes())    // sender
		h.Write(ethToken.Bytes())  // tokenAddress (ETH = zero address)
		h.Write(amountFixed[:])    // amount (fixed 32 bytes)
		h.Write(recipient.Bytes()) // to
		var expectedSubType [32]byte
		copy(expectedSubType[:], h.Sum(nil))
		assert.Equal(t, expectedSubType, appEvents[0].EventSubType, "EventSubType should carry the raw 32-byte receipt hash")
		assert.Nil(t, appEvents[0].Data, "Data should be nil when receipt hash is carried in EventSubType")

	})
}

func TestWasmtimeRuntime_ProcessRequest_Withdrawal(t *testing.T) {
	type TestWithdrawalEventData struct {
		Type         string            `json:"type"`
		To           ethCommon.Address `json:"to"`
		TokenAddress ethCommon.Address `json:"tokenAddress"`
		Amount       *common.Big       `json:"amount"`
	}

	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	depositAmount := big.NewInt(1000000000000000000) // 1 ETH
	withdrawValue := big.NewInt(500000000000000000)  // 0.5 ETH
	withdrawAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")

	initialState, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	stateAfterDeposit, _, _, fuel, failure := runtime.Deposit(ctx, appId, sender, ethToken, depositAmount, initialState, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	// Create withdrawal payload
	withdrawPayload := payloadInstructions{
		Type: "withdraw",
		Withdraw: &withdrawInstruction{
			To:     withdrawAddress,
			Amount: common.ToBig(withdrawValue),
		},
	}
	payloadBytes, err := json.Marshal(withdrawPayload)
	require.NoError(t, err, "Should marshal withdrawal payload")

	newState, events, _, withdrawals, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, sender, common.Process, payloadBytes, stateAfterDeposit, wasmBytes)
	require.Nil(t, failure)
	require.NotNil(t, newState, "New state should not be nil")
	require.Len(t, events, 1, "Should generate one event")
	require.Len(t, withdrawals, 1, "Should generate one withdrawal")
	require.Nil(t, reportBytes, "Report should be nil for withdrawal requests")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

	// Verify withdrawal event. EventSubType is the zero value at the runtime
	// level — see the note in TestWasmtimeRuntime_Deposit.
	event := events[0]
	assert.Equal(t, sender, event.UserID)
	assert.Equal(t, [32]byte{}, event.EventSubType)

	var eventData TestWithdrawalEventData
	err = json.Unmarshal(event.Data, &eventData)
	require.NoError(t, err, "Event data should be valid JSON")
	assert.Equal(t, "withdrawal", eventData.Type)
	assert.Equal(t, withdrawAddress, eventData.To)
	assert.Equal(t, withdrawValue, eventData.Amount.ToInt())

	// Verify withdrawal
	withdrawal := withdrawals[0]
	assert.Equal(t, withdrawAddress, withdrawal.DestinationAddress)
	assert.Equal(t, withdrawValue, withdrawal.Amount.ToInt())
	assert.Equal(t, ethToken, withdrawal.TokenAddress, "ETH withdrawal should have zero token address")

	// Verify the state was updated
	var stateData applicationInternalState
	err = json.Unmarshal(newState, &stateData)
	require.NoError(t, err, "New state should be valid JSON")
	updatedBalance := new(big.Int).Sub(depositAmount, withdrawValue)

	assert.Equal(t, updatedBalance, getTestBalance(stateData, sender, ethAddressHex()))
}

func TestWasmtimeRuntime_ProcessRequest_Deanonymize(t *testing.T) {
	type TestDeanonymizationReport struct {
		Accounts map[ethCommon.Address]testAccountState `json:"accounts"`
		Nonce    uint64                                 `json:"nonce"`
	}

	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	depositAmount := big.NewInt(1000000000000000000) // 1 ETH

	initialState, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	stateWithData, _, _, fuel, failure := runtime.Deposit(ctx, appId, sender, ethToken, depositAmount, initialState, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	_, _, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, sender, common.Deanonymize, []byte("{}"), stateWithData, wasmBytes)
	require.Nil(t, failure)
	require.NotNil(t, reportBytes, "Report should not be nil")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(20)))

	var reportData TestDeanonymizationReport
	err = json.Unmarshal(reportBytes, &reportData)
	require.NoError(t, err, "Report should be valid JSON")

	require.Contains(t, reportData.Accounts, sender)
	ethHex := ethAddressHex()
	require.NotNil(t, reportData.Accounts[sender].Balances[ethHex])
	assert.Equal(t, depositAmount, reportData.Accounts[sender].Balances[ethHex].ToInt())
}

func TestWasmtimeRuntime_FullWorkflow(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	user2 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))

	t.Log("Step 1: Load module")
	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err, "LoadModule should succeed")
	require.NotNil(t, state)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	t.Log("Step 2: Make deposit for user1")
	depositAmount := big.NewInt(2000000000000000000) // 2 ETH
	state, events, _, fuel, failure := runtime.Deposit(ctx, appId, user1, ethToken, depositAmount, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 1)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	t.Log("Step 3: Transfer from user1 to user2")
	transferValue := big.NewInt(500000000000000000) // 0.5 ETH
	transferPayload := payloadInstructions{
		Type: "transfer",
		Transfer: &transferInstruction{
			To:     user2,
			Amount: common.ToBig(transferValue),
		},
	}
	payloadBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)

	var reportBytes []byte
	state, events, _, withdrawals, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Process, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 2)
	require.Len(t, withdrawals, 0)
	require.Nil(t, reportBytes, "Report should be nil for transfer requests")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

	t.Log("Step 4: Withdraw from user2")
	withdrawValue := big.NewInt(250000000000000000) // 0.25 ETH
	withdrawAddress := ethCommon.HexToAddress("0x1234567890123456789012345678901234567890")
	withdrawPayload := payloadInstructions{
		Type: "withdraw",
		Withdraw: &withdrawInstruction{
			To:     withdrawAddress,
			Amount: common.ToBig(withdrawValue),
		},
	}
	payloadBytes, err = json.Marshal(withdrawPayload)
	require.NoError(t, err)

	state, events, _, withdrawals, reportBytes, fuel, failure = runtime.ProcessRequest(ctx, appId, user2, common.Process, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	require.Len(t, events, 1)
	require.Len(t, withdrawals, 1)
	require.Nil(t, reportBytes, "Report should be nil for withdrawal requests")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))

	t.Log("Step 5: Generate deanonymization report via ProcessRequest")
	_, _, _, _, report, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Deanonymize, []byte("{}"), state, wasmBytes)
	require.Nil(t, failure)
	require.NotNil(t, report)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(20)))

	var stateData applicationInternalState
	err = json.Unmarshal(state, &stateData)
	require.NoError(t, err)

	ethHex := ethAddressHex()
	user1UpdatedBalance := new(big.Int).Sub(depositAmount, transferValue)
	user2UpdatedBalance := new(big.Int).Sub(transferValue, withdrawValue)
	assert.Equal(t, user1UpdatedBalance, getTestBalance(stateData, user1, ethHex))
	assert.Equal(t, user2UpdatedBalance, getTestBalance(stateData, user2, ethHex))

	t.Log("Full workflow completed successfully!")
}

func TestWasmtimeRuntime_ConcurrentModuleLoading(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	var wg sync.WaitGroup
	errors := make(chan error, 3)

	for i := range 3 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			appId := common.NewApplicationId(uint64(id))
			_, _, err := runtime.LoadModule(ctx, appId, wasmBytes)
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
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	// Make 100 deposits to create a large state
	for i := range 100 {
		user := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", i))

		depositAmount := big.NewInt(1000000000000000000) // 1 ETH
		newState, _, _, _, failure := runtime.Deposit(ctx, appId, user, ethToken, depositAmount, state, wasmBytes)
		require.Nil(t, failure)
		state = newState
	}

	// Verify the large state can still be processed
	transferPayload := payloadInstructions{
		Type: "transfer",
		Transfer: &transferInstruction{
			To:     ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1)),
			Amount: common.ToBig(big.NewInt(500000000000000000)),
		},
	}
	payloadBytes, err := json.Marshal(transferPayload)
	require.NoError(t, err)

	sender := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 0))
	_, events, _, withdrawals, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, sender, common.Process, payloadBytes, state, wasmBytes)
	require.Nil(t, failure)
	assert.Len(t, events, 2)
	assert.Len(t, withdrawals, 0)
	require.Nil(t, reportBytes, "Report should be nil for transfer requests")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(50)))
}

func TestWasmtimeRuntime_InvalidWasmModule(t *testing.T) {
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	invalidWasm := []byte("invalid wasm bytes")

	_, fuel, err := runtime.LoadModule(ctx, appId, invalidWasm)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile WASM module")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
}

func TestWasmtimeRuntime_EmptyWasmModule(t *testing.T) {
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	emptyWasm := []byte{}

	_, fuel, err := runtime.LoadModule(ctx, appId, emptyWasm)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile WASM module")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
}

func TestWasmtimeRuntime_NilInputs(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	appId := common.NewApplicationId(1)

	t.Run("NilWasmBytes", func(t *testing.T) {
		_, fuel, err := runtime.LoadModule(ctx, appId, nil)
		assert.Error(t, err)
		require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
	})

	t.Run("InvalidAppId", func(t *testing.T) {
		appId := common.ApplicationIdType(math.MaxInt64 + 1)
		_, _, err := runtime.LoadModule(ctx, appId, wasmBytes)
		if err == nil {
			_, _, _, fuel, failure := runtime.Deposit(ctx, appId, user1, ethToken, big.NewInt(1000), []byte("{}"), wasmBytes)
			assert.NotNil(t, failure)
			require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
		}
	})

	t.Run("NilState", func(t *testing.T) {
		appId := common.NewApplicationId(1)
		_, _, _, fuel, failure := runtime.Deposit(ctx, appId, user1, ethToken, big.NewInt(1000), nil, wasmBytes)
		assert.NotNil(t, failure)
		require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
	})
}

func TestWasmtimeRuntime_InvalidPayloads(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	user2 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	t.Run("InvalidJSON", func(t *testing.T) {
		invalidPayload := []byte("invalid json")
		_, _, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Process, invalidPayload, state, wasmBytes)
		assert.NotNil(t, failure)
		assert.Contains(t, failure.Error(), "Failed to parse payload instructions")
		require.Nil(t, reportBytes, "Report should be nil on error")
		require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
	})

	t.Run("MissingTransferFields", func(t *testing.T) {
		incompletePayload := map[string]interface{}{
			"type": "transfer",
		}
		payloadBytes, _ := json.Marshal(incompletePayload)
		_, _, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Process, payloadBytes, state, wasmBytes)
		assert.NotNil(t, failure)
		assert.Contains(t, failure.Error(), "Transfer instruction is missing")
		require.Nil(t, reportBytes, "Report should be nil on error")
		require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
	})

	t.Run("AccountDoesNotExist", func(t *testing.T) {
		negativePayload := map[string]interface{}{
			"type": "transfer",
			"transfer": map[string]interface{}{
				"to":     user2,
				"amount": common.NewBig(uint64(500)),
			},
		}
		payloadBytes, _ := json.Marshal(negativePayload)
		_, _, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Process, payloadBytes, state, wasmBytes)
		assert.NotNil(t, failure)
		assert.Contains(t, strings.ToLower(failure.Error()), fmt.Sprintf("account %s does not exist!", strings.ToLower(user1.String())))
		require.Nil(t, reportBytes, "Report should be nil on error")
		require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
	})
}

func TestWasmtimeRuntime_InsufficientFunds(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	user2 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 2))
	depositAmount := new(big.Int).SetUint64(12345678901234567890) // fits in uint64, > max int64

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	deposit := new(big.Int).Div(depositAmount, big.NewInt(2))
	state, _, _, fuel, failure := runtime.Deposit(ctx, appId, user1, ethToken, deposit, state, wasmBytes)
	require.Nil(t, failure)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))

	// Try to transfer without enough funds
	transferPayload := payloadInstructions{
		Type: "transfer",
		Transfer: &transferInstruction{
			To:     user2,
			Amount: common.ToBig(depositAmount),
		},
	}
	payloadBytes, _ := json.Marshal(transferPayload)

	_, _, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Process, payloadBytes, state, wasmBytes)
	assert.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "Insufficient balance for transfer")
	require.Nil(t, reportBytes, "Report should be nil on error")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
}

func TestWasmtimeRuntime_LargePayload(t *testing.T) {
	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
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

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	_, _, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Process, largePayload, state, wasmBytes)
	// should not panic but return an error that the payload does not conform to the expected format
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "Failed to parse payload instructions")
	require.Nil(t, reportBytes, "Report should be nil on error")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
}

func TestWasmtimeRuntime_InvalidStateFormat(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))

	corruptedState := []byte("corrupted state data")

	state, events, _, fuel, failure := runtime.Deposit(ctx, appId, user1, ethToken, big.NewInt(1000), corruptedState, wasmBytes)
	require.NotNil(t, failure)
	require.Contains(t, failure.Error(), "Failed to parse application state")
	require.Equal(t, []byte(nil), state)
	require.Equal(t, []common.PlainEvent(nil), events)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
}

func TestWasmtimeRuntime_MultipleLoadModule(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _, err := runtime.LoadModule(ctx, common.NewApplicationId(uint64(i)), wasmBytes)
		require.NoError(t, err)
	}
}

func TestWasmtimeRuntime_ZeroValueOperations(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)
	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	// Test zero depositAmount deposit
	newState, events, _, fuel, failure := runtime.Deposit(ctx, appId, user1, ethToken, big.NewInt(0), state, wasmBytes)

	require.Nil(t, failure)
	require.Len(t, events, 0, "Zero depositAmount deposit should not generate any events")
	require.NotNil(t, newState, "State should not be nil after zero depositAmount deposit")
	require.Equal(t, state, newState)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(35)))
}

func TestWasmtimeRuntime_InvalidInstruction(t *testing.T) {
	wasmBytes := readWasm(t)

	runtime := wasm.NewWasmtimeRuntime(newTestLogger(), 0)
	defer runtime.Close()

	ctx := context.Background()
	appId := common.NewApplicationId(1)

	state, fuel, err := runtime.LoadModule(ctx, appId, wasmBytes)
	require.NoError(t, err)
	require.Equal(t, 0, fuel.Cmp(big.NewInt(5)))

	invalidPayload := map[string]interface{}{
		"type": "unsupported_instruction",
	}
	payloadBytes, err := json.Marshal(invalidPayload)
	require.NoError(t, err)

	user1 := ethCommon.HexToAddress(fmt.Sprintf("0xadd%037x", 1))
	_, _, _, _, reportBytes, fuel, failure := runtime.ProcessRequest(ctx, appId, user1, common.Process, payloadBytes, state, wasmBytes)
	assert.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "Unsupported instruction type")
	require.Nil(t, reportBytes, "Report should be nil on error")
	require.Equal(t, 0, fuel.Cmp(big.NewInt(0)))
}
