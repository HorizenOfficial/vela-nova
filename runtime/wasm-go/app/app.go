package app

import (
	"encoding/json"
	"fmt"

	"github.com/horizen-pes-nova/payment-app/utils"
)

// --- High-Level Application Logic ---

func LoadModule(appId int64) LoadModuleResult {
	initialState := &ApplicationInternalState{
		AppID:    appId,
		Accounts: make(map[string]*AccountState),
	}
	stateJSON, err := json.Marshal(initialState)
	if err != nil {
		return LoadModuleResult{
			Error: fmt.Sprintf("failed to marshal initial state: %v", err),
		}
	}
	return LoadModuleResult{
		State: stateJSON,
		Fuel:  NewUint256(5),
	}
}

func DepositFunds(senderPtr *Address, value *Uint256, stateJSON string) DepositResult {
	if senderPtr == nil {
		return DepositResult{Error: "Sender address is nil"}
	}

	//This should never happens but just in case
	if value == nil {
		return DepositResult{Error: "value is nil"}
	}

	fmt.Printf("DepositFunds called with address %s, value %s\n", senderPtr.String(), value.String())

	senderHex := senderPtr.Hex()

	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		return DepositResult{Error: "Failed to parse application state"}
	}

	var events []PlainEvent

	// Handle deposit only if value > 0
	if !value.IsZero() {
		// Ensure sender account exists
		if currentState.Accounts[senderHex] == nil {
			currentState.Accounts[senderHex] = &AccountState{
				Address: *senderPtr,
				Balance: NewUint256(0),
			}
		}

		// Add deposit to sender's balance
		if currentState.Accounts[senderHex].Balance.AddOverflow(*currentState.Accounts[senderHex].Balance, *value) {
			return DepositResult{Error: fmt.Sprintf("Overflow while adding amount %s to balance: %s", value, currentState.Accounts[senderHex].Balance)}
		}
		currentState.Nonce++

		// Create deposit event
		eventData := DepositEvent{
			Type:    "deposit",
			Amount:  value,
			Balance: currentState.Accounts[senderHex].Balance,
			Nonce:   currentState.Nonce,
		}
		eventDataBytes, err := json.Marshal(eventData)
		if err != nil {
			return DepositResult{Error: "Failed to serialize event data"}
		}

		events = append(events, PlainEvent{
			UserID:       *senderPtr,
			EventSubType: "deposit",
			Data:         eventDataBytes,
		})
	}

	// Serialize the updated state
	newStateBytes, err := json.Marshal(&currentState)
	if err != nil {
		return DepositResult{Error: "Failed to serialize new state"}
	}
	return DepositResult{State: newStateBytes, Events: events, Fuel: NewUint256(35)}
}

func ProcessRequest(senderPtr *Address, payloadJSON, stateJSON string) ProcessResult {
	if senderPtr == nil {
		return ProcessResult{Error: "Sender address is missing"}
	}

	sender := *senderPtr
	senderHex := sender.Hex()

	// Deserialize current state
	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		return ProcessResult{Error: "Failed to parse application state"}
	}

	var events []PlainEvent
	var withdrawals []Withdrawal

	// Process payload instructions if payload is not empty
	if payloadJSON != "" {
		var instructions PayloadInstructions
		if err := json.Unmarshal([]byte(payloadJSON), &instructions); err != nil {
			return ProcessResult{Error: "Failed to parse payload instructions"}
		}

		switch instructions.Type {
		case "transfer":
			if instructions.Transfer == nil {
				return ProcessResult{Error: "Transfer instruction is missing"}
			}
			if instructions.Transfer.Amount == nil {
				return ProcessResult{Error: "Transfer amount is nil"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				return ProcessResult{Error: fmt.Sprintf("Account does not exist: %s", senderHex)}
			}
			if currentState.Accounts[senderHex].Balance.Cmp(*instructions.Transfer.Amount) < 0 {
				return ProcessResult{Error: "Insufficient balance for transfer"}
			}

			recipientHex := instructions.Transfer.To.Hex()

			// Ensure recipient account exists
			if currentState.Accounts[recipientHex] == nil {
				currentState.Accounts[recipientHex] = &AccountState{
					Address: instructions.Transfer.To,
					Balance: NewUint256(0),
				}
			}

			// Execute transfer
			currentState.Accounts[senderHex].Balance.Sub(*currentState.Accounts[senderHex].Balance, *instructions.Transfer.Amount)
			currentState.Accounts[recipientHex].Balance.Add(*currentState.Accounts[recipientHex].Balance, *instructions.Transfer.Amount)
			currentState.Nonce++

			// Create events for both parties
			senderEventData := SenderEvent{
				Type:    "transfer_sent",
				To:      instructions.Transfer.To,
				Amount:  instructions.Transfer.Amount,
				Balance: currentState.Accounts[senderHex].Balance,
				Nonce:   currentState.Nonce,
			}
			senderEventDataBytes, err := json.Marshal(senderEventData)
			if err != nil {
				return ProcessResult{Error: "Failed to serialize sender event data"}
			}

			recipientEventData := RecipientEvent{
				Type:    "transfer_received",
				From:    sender,
				Amount:  instructions.Transfer.Amount,
				Balance: currentState.Accounts[recipientHex].Balance,
				Nonce:   currentState.Nonce,
			}
			recipientEventDataBytes, err := json.Marshal(recipientEventData)
			if err != nil {
				return ProcessResult{Error: "Failed to serialize recipient event data"}
			}

			events = append(events, PlainEvent{
				UserID:       sender,
				EventSubType: "transfer_sent",
				Data:         senderEventDataBytes,
			})

			events = append(events, PlainEvent{
				UserID:       instructions.Transfer.To,
				EventSubType: "transfer_received",
				Data:         recipientEventDataBytes,
			})

		case "withdraw":
			if instructions.Withdraw == nil {
				return ProcessResult{Error: "Withdraw instruction is missing"}
			}
			if instructions.Withdraw.Amount == nil {
				return ProcessResult{Error: "Withdraw amount is nil"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				return ProcessResult{Error: "Account does not exist"}
			}

			if currentState.Accounts[senderHex].Balance.Cmp(*instructions.Withdraw.Amount) < 0 {
				return ProcessResult{Error: "Insufficient balance for withdrawal"}
			}

			// Execute withdrawal
			currentState.Accounts[senderHex].Balance.Sub(*currentState.Accounts[senderHex].Balance, *instructions.Withdraw.Amount)
			currentState.Nonce++

			// Create withdrawal
			withdrawals = append(withdrawals, Withdrawal{
				DestinationAddress: instructions.Withdraw.To,
				Amount:             instructions.Withdraw.Amount,
			})

			// Create event for sender
			withdrawEventData := WithdrawalEvent{
				Type:    "withdrawal",
				To:      instructions.Withdraw.To,
				Amount:  instructions.Withdraw.Amount,
				Balance: currentState.Accounts[senderHex].Balance,
				Nonce:   currentState.Nonce,
			}
			withdrawEventDataBytes, err := json.Marshal(withdrawEventData)
			if err != nil {
				return ProcessResult{Error: "Failed to serialize withdraw event data"}
			}

			events = append(events, PlainEvent{
				UserID:       sender,
				EventSubType: "withdrawal",
				Data:         withdrawEventDataBytes,
			})

		default:
			return ProcessResult{Error: "Unsupported instruction type"}
		}
	}

	// Serialize the updated state
	newStateBytes, err := json.Marshal(currentState)
	if err != nil {
		return ProcessResult{Error: "Failed to serialize new state"}
	}
	return ProcessResult{
		State:       newStateBytes,
		Events:      events,
		Withdrawals: withdrawals,
		Fuel:        NewUint256(50),
	}
}

func GenerateDeanonymizationReport(payloadJSON, stateJSON string) DeanonymizationResult {
	// Deserialize payload
	var payload ReportPayloadInstructions
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return DeanonymizationResult{Error: fmt.Sprintf("Failed to parse payload: %s", payloadJSON)}
	}

	// Deserialize current state
	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		return DeanonymizationResult{Error: "Failed to parse application state"}
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
		return DeanonymizationResult{Error: "Failed to serialize deanonymization report"}
	}
	return DeanonymizationResult{Report: reportBytes, Fuel: NewUint256(20)}
}

func GetAllocatedMemoryStats() MemoryStats {
	map_size, total_bytes := utils.GetAllocatedMemoryStats()
	return MemoryStats{
		MapSize:              map_size,
		CumulativeMemorySize: total_bytes,
	}
}
