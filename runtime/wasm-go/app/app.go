package app

import (
	"encoding/json"
	"fmt"
	"math/big"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/horizen-pes/pkg/common"
	wasmCommon "github.com/horizen-pes/pkg/wasm/common"
)

// --- High-Level Application Logic ---

func LoadModule(appId int64) wasmCommon.LoadModuleResult {
	initialState := &ApplicationInternalState{
		AppID:    appId,
		Accounts: make(map[ethCommon.Address]*AccountState),
	}
	stateJSON, err := json.Marshal(initialState)
	if err != nil {
		return wasmCommon.LoadModuleResult{
			Error: fmt.Sprintf("failed to marshal initial state: %v", err),
		}
	}
	return wasmCommon.LoadModuleResult{
		State: stateJSON,
		Fuel:  big.NewInt(5),
	}
}

func DepositFunds(senderPtr *ethCommon.Address, depositAmount *big.Int, stateJSON string) wasmCommon.DepositResult {
	if senderPtr == nil {
		return wasmCommon.DepositResult{Error: "Sender address is missing"}
	}

	sender := *senderPtr
	//This should never happens but just in case
	if depositAmount == nil {
		return wasmCommon.DepositResult{Error: "value is nil"}
	}

	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		return wasmCommon.DepositResult{Error: "Failed to parse application state"}
	}

	var events []common.PlainEvent

	// Handle deposit
	if depositAmount.Sign() > 0 {
		// Ensure sender account exists
		if currentState.Accounts[sender] == nil {
			currentState.Accounts[sender] = &AccountState{
				Address: sender,
				Balance: big.NewInt(0),
			}
		}

		// Add deposit to sender's balance
		currentState.Accounts[sender].Balance.Add(currentState.Accounts[sender].Balance, depositAmount)
		currentState.Nonce++

		// Create deposit event
		eventData := wasmCommon.DepositEvent{
			Type:    "deposit",
			Amount:  depositAmount,
			Balance: currentState.Accounts[sender].Balance,
			Nonce:   currentState.Nonce,
		}
		eventDataBytes, err := json.Marshal(eventData)
		if err != nil {
			return wasmCommon.DepositResult{Error: "Failed to serialize event data"}
		}

		events = append(events, common.PlainEvent{
			UserID:       sender,
			EventSubType: "deposit",
			Data:         eventDataBytes,
		})
	}

	// Serialize the updated state
	newStateBytes, err := json.Marshal(&currentState)
	if err != nil {
		return wasmCommon.DepositResult{Error: "Failed to serialize new state"}
	}
	return wasmCommon.DepositResult{State: newStateBytes, Events: events, Fuel: big.NewInt(35)}
}

func ProcessRequest(senderPtr *ethCommon.Address, payloadJSON, stateJSON string) wasmCommon.ProcessResult {
	if senderPtr == nil {
		return wasmCommon.ProcessResult{Error: "Sender address is missing"}
	}
	sender := *senderPtr
	// Deserialize current state
	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		return wasmCommon.ProcessResult{Error: "Failed to parse application state"}
	}

	var events []common.PlainEvent
	var withdrawals []common.Withdrawal

	// Process payload instructions if payload is not empty
	if payloadJSON != "" {
		var instructions PayloadInstructions
		if err := json.Unmarshal([]byte(payloadJSON), &instructions); err != nil {
			return wasmCommon.ProcessResult{Error: "Failed to parse payload instructions"}
		}

		switch instructions.Type {
		case "transfer":
			if instructions.Transfer == nil {
				return wasmCommon.ProcessResult{Error: "Transfer instruction is missing"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[sender] == nil {
				return wasmCommon.ProcessResult{Error: fmt.Sprintf("Account does not exist: %s", sender.Hex())}
			}
			if currentState.Accounts[sender].Balance.Cmp(instructions.Transfer.Amount) < 0 {
				return wasmCommon.ProcessResult{Error: "Insufficient balance for transfer"}
			}

			// Ensure recipient account exists
			if currentState.Accounts[instructions.Transfer.To] == nil {
				currentState.Accounts[instructions.Transfer.To] = &AccountState{
					Address: instructions.Transfer.To,
					Balance: big.NewInt(0),
				}
			}

			// Execute transfer
			currentState.Accounts[sender].Balance.Sub(currentState.Accounts[sender].Balance, instructions.Transfer.Amount)
			currentState.Accounts[instructions.Transfer.To].Balance.Add(currentState.Accounts[instructions.Transfer.To].Balance, instructions.Transfer.Amount)
			currentState.Nonce++

			// Create events for both parties
			senderEventData := wasmCommon.SenderEvent{
				Type:    "transfer_sent",
				To:      instructions.Transfer.To,
				Amount:  instructions.Transfer.Amount,
				Balance: currentState.Accounts[sender].Balance,
				Nonce:   currentState.Nonce,
			}
			senderEventDataBytes, err := json.Marshal(senderEventData)
			if err != nil {
				return wasmCommon.ProcessResult{Error: "Failed to serialize sender event data"}
			}

			recipientEventData := wasmCommon.RecipientEvent{
				Type:    "transfer_received",
				From:    sender,
				Amount:  instructions.Transfer.Amount,
				Balance: currentState.Accounts[instructions.Transfer.To].Balance,
				Nonce:   currentState.Nonce,
			}
			recipientEventDataBytes, err := json.Marshal(recipientEventData)
			if err != nil {
				return wasmCommon.ProcessResult{Error: "Failed to serialize recipient event data"}
			}

			events = append(events, common.PlainEvent{
				UserID:       sender,
				EventSubType: "transfer_sent",
				Data:         senderEventDataBytes,
			})

			events = append(events, common.PlainEvent{
				UserID:       instructions.Transfer.To,
				EventSubType: "transfer_received",
				Data:         recipientEventDataBytes,
			})

		case "withdraw":
			if instructions.Withdraw == nil {
				return wasmCommon.ProcessResult{Error: "Withdraw instruction is missing"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[sender] == nil {
				return wasmCommon.ProcessResult{Error: "Account does not exist"}
			}

			if currentState.Accounts[sender].Balance.Cmp(instructions.Withdraw.Amount) < 0 {
				return wasmCommon.ProcessResult{Error: "Insufficient balance for withdrawal"}
			}

			// Execute withdrawal
			currentState.Accounts[sender].Balance.Sub(currentState.Accounts[sender].Balance, instructions.Withdraw.Amount)
			currentState.Nonce++

			// Create withdrawal
			withdrawals = append(withdrawals, common.Withdrawal{
				DestinationAddress: instructions.Withdraw.To,
				Amount:             instructions.Withdraw.Amount,
			})

			// Create event for sender
			withdrawEventData := wasmCommon.WithdrawalEvent{
				Type:    "withdrawal",
				To:      instructions.Withdraw.To,
				Amount:  instructions.Withdraw.Amount,
				Balance: currentState.Accounts[sender].Balance,
				Nonce:   currentState.Nonce,
			}
			withdrawEventDataBytes, err := json.Marshal(withdrawEventData)
			if err != nil {
				return wasmCommon.ProcessResult{Error: "Failed to serialize withdraw event data"}
			}

			events = append(events, common.PlainEvent{
				UserID:       sender,
				EventSubType: "withdrawal",
				Data:         withdrawEventDataBytes,
			})

		default:
			return wasmCommon.ProcessResult{Error: "Unsupported instruction type"}
		}
	}

	// Serialize the updated state
	newStateBytes, err := json.Marshal(currentState)
	if err != nil {
		return wasmCommon.ProcessResult{Error: "Failed to serialize new state"}
	}
	return wasmCommon.ProcessResult{
		State:       newStateBytes,
		Events:      events,
		Withdrawals: withdrawals,
		Fuel:        big.NewInt(50),
	}
}

func GenerateDeanonymizationReport(payloadJSON, stateJSON string) wasmCommon.DeanonymizationResult {
	// Deserialize payload
	var payload ReportPayloadInstructions
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return wasmCommon.DeanonymizationResult{Error: fmt.Sprintf("Failed to parse payload: %s", payloadJSON)}
	}

	// Deserialize current state
	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		return wasmCommon.DeanonymizationResult{Error: "Failed to parse application state"}
	}

	// Create deanonymization report
	report := UnencryptedDeanonymizationReportData{
		Accounts: currentState.Accounts,
		Nonce:    currentState.Nonce,
	}

	// read contents of the payload and decide how to build the report.
	//if payload.... TODO

	// Serialize the report
	reportBytes, err := json.Marshal(report)
	if err != nil {
		return wasmCommon.DeanonymizationResult{Error: "Failed to serialize deanonymization report"}
	}
	return wasmCommon.DeanonymizationResult{Report: reportBytes, Fuel: big.NewInt(20)}
}

// AccountState represents the state of a user account
type AccountState struct {
	Address ethCommon.Address `json:"address"`
	Balance *big.Int          `json:"balance"`
}

// ApplicationInternalState represents the internal state of the application
type ApplicationInternalState struct {
	AppID    int64                               `json:"appId"`
	Accounts map[ethCommon.Address]*AccountState `json:"accounts"`
	Nonce    uint64                              `json:"nonce"`
}

// TransferInstruction represents instructions for transferring funds
type TransferInstruction struct {
	To     ethCommon.Address `json:"to"`
	Amount *big.Int          `json:"amount"`
}

// WithdrawInstruction represents instructions for withdrawing funds
type WithdrawInstruction struct {
	To     ethCommon.Address `json:"to"`
	Amount *big.Int          `json:"amount"`
}

// PayloadInstructions represents the deserialized payload instructions
type PayloadInstructions struct {
	Type     string               `json:"type"`
	Transfer *TransferInstruction `json:"transfer,omitempty"`
	Withdraw *WithdrawInstruction `json:"withdraw,omitempty"`
}

type UnencryptedDeanonymizationReportData struct {
	Accounts map[ethCommon.Address]*AccountState `json:"accounts"`
	Nonce    uint64                              `json:"nonce"`
}

// ReportPayloadInstructions represents a specific information on how to generate a report
// TODO - We can add the list of the accounts to be included in the report and a boolean specifying whether
// we can omit empty accounts
type ReportPayloadInstructions struct {
}
