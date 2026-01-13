package app

import (
	"encoding/json"
	"fmt"
	"math/big"
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
		Fuel:  big.NewInt(5),
	}
}

func DepositFunds(senderPtr *Address, depositAmount *big.Int, stateJSON string) DepositResult {
	if senderPtr == nil {
		return DepositResult{Error: "Sender address is nil"}
	}

	//This should never happens but just in case
	if depositAmount == nil {
		return DepositResult{Error: "value is nil"}
	}

	senderHex := senderPtr.Hex()

	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		return DepositResult{Error: "Failed to parse application state"}
	}

	var events []PlainEvent

	// Handle deposit
	if depositAmount.Sign() > 0 {
		// Ensure sender account exists
		if currentState.Accounts[senderHex] == nil {
			currentState.Accounts[senderHex] = &AccountState{
				Address: *senderPtr,
				Balance: big.NewInt(0),
			}
		}

		// Add deposit to sender's balance
		currentState.Accounts[senderHex].Balance.Add(currentState.Accounts[senderHex].Balance, depositAmount)
		currentState.Nonce++

		// Create deposit event
		eventData := DepositEvent{
			Type:    "deposit",
			Amount:  depositAmount,
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
	return DepositResult{State: newStateBytes, Events: events, Fuel: big.NewInt(35)}
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

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				return ProcessResult{Error: fmt.Sprintf("Account does not exist: %s", sender.Hex())}
			}
			if currentState.Accounts[senderHex].Balance.Cmp(instructions.Transfer.Amount) < 0 {
				return ProcessResult{Error: "Insufficient balance for transfer"}
			}

			// Ensure recipient account exists
			if currentState.Accounts[instructions.Transfer.To.Hex()] == nil {
				currentState.Accounts[instructions.Transfer.To.Hex()] = &AccountState{
					Address: instructions.Transfer.To,
					Balance: big.NewInt(0),
				}
			}

			// Execute transfer
			currentState.Accounts[senderHex].Balance.Sub(currentState.Accounts[senderHex].Balance, instructions.Transfer.Amount)
			currentState.Accounts[instructions.Transfer.To.Hex()].Balance.Add(currentState.Accounts[instructions.Transfer.To.Hex()].Balance, instructions.Transfer.Amount)
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
				Balance: currentState.Accounts[instructions.Transfer.To.Hex()].Balance,
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

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				return ProcessResult{Error: "Account does not exist"}
			}

			if currentState.Accounts[senderHex].Balance.Cmp(instructions.Withdraw.Amount) < 0 {
				return ProcessResult{Error: "Insufficient balance for withdrawal"}
			}

			// Execute withdrawal
			currentState.Accounts[senderHex].Balance.Sub(currentState.Accounts[senderHex].Balance, instructions.Withdraw.Amount)
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
		Fuel:        big.NewInt(50),
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
	return DeanonymizationResult{Report: reportBytes, Fuel: big.NewInt(20)}
}
