package app

import (
	"encoding/json"
	"fmt"
	"time"

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
		utils.LogError("LoadModule: failed to marshal initial state: %v", err)
		return LoadModuleResult{
			Error: fmt.Sprintf("failed to marshal initial state: %v", err),
		}
	}
	fuel := NewUint256(5)
	utils.LogDebug("LoadModule: appId=%d, stateSize=%d, fuel=%v", appId, len(stateJSON), fuel)
	return LoadModuleResult{
		State: stateJSON,
		Fuel:  fuel,
	}
}

func DepositFunds(senderPtr *Address, value *Uint256, stateJSON string) DepositResult {
	if senderPtr == nil {
		utils.LogError("DepositFunds: sender address is nil")
		return DepositResult{Error: "Sender address is nil"}
	}

	//This should never happens but just in case
	if value == nil {
		utils.LogError("DepositFunds: value is nil")
		return DepositResult{Error: "value is nil"}
	}

	utils.LogDebug("DepositFunds called with address %s, value %s", senderPtr.String(), value.String())

	senderHex := senderPtr.Hex()

	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		// we could add the stateJSON to the error, but it is not safe if it is very large
		utils.LogError("DepositFunds: failed to parse application state: %v", err)
		return DepositResult{Error: fmt.Sprintf("Failed to parse application state: %v", err)}
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

		// Add deposit to sender's balance (copy old balance by value for revert on overflow)
		oldBalance := *currentState.Accounts[senderHex].Balance
		if currentState.Accounts[senderHex].Balance.AddOverflow(*currentState.Accounts[senderHex].Balance, *value) {
			utils.LogError("DepositFunds: overflow while adding amount %s to balance %s for account %s", value.String(), oldBalance.String(), senderHex)
			*currentState.Accounts[senderHex].Balance = oldBalance // revert to previous balance
			return DepositResult{Error: fmt.Sprintf("Overflow while adding amount %s to balance: %s", value, oldBalance)}
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
			utils.LogError("DepositFunds: failed to serialize event data: %v", err)
			return DepositResult{Error: fmt.Sprintf("Failed to serialize event data: %+v, err: %v", eventData, err)}
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
		utils.LogError("DepositFunds: failed to serialize new state: %v", err)
		return DepositResult{Error: fmt.Sprintf("Failed to serialize new state: %v", err)}
	}

	// Get balance string safely (account may not exist, for instance in case of zero deposits)
	var balanceStr string
	if currentState.Accounts[senderHex] != nil {
		balanceStr = currentState.Accounts[senderHex].Balance.String()
	} else {
		balanceStr = "0"
	}

	fuel := NewUint256(35)
	utils.LogDebug("DepositFunds: sender=%s, value=%s, newBalance=%s, eventsCount=%d, stateSize=%d, fuel=%s",
		senderHex, value.String(), balanceStr, len(events), len(newStateBytes), fuel.String())
	return DepositResult{State: newStateBytes, Events: events, Fuel: fuel}
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
		utils.LogError("ProcessRequest: failed to parse application state: %v", err)
		return ProcessResult{Error: fmt.Sprintf("Failed to parse application state: %v", err)}
	}

	var events []PlainEvent
	var withdrawals []Withdrawal

	// Process payload instructions if payload is not empty
	if payloadJSON != "" {
		var instructions PayloadInstructions
		if err := json.Unmarshal([]byte(payloadJSON), &instructions); err != nil {
			utils.LogError("ProcessRequest: failed to parse payload instructions: %v", err)
			return ProcessResult{Error: fmt.Sprintf("Failed to parse payload instructions: %v", err)}
		}

		switch instructions.Type {
		case "transfer":
			if instructions.Transfer == nil {
				utils.LogError("ProcessRequest: transfer instruction is missing in payload")
				return ProcessResult{Error: "Transfer instruction is missing"}
			}
			if instructions.Transfer.Amount == nil {
				utils.LogError("ProcessRequest: transfer amount is nil")
				return ProcessResult{Error: "Transfer amount is nil"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				utils.LogError("ProcessRequest: account %s does not exist", senderHex)
				return ProcessResult{Error: fmt.Sprintf("Account %s does not exist!", senderHex)}
			}
			if currentState.Accounts[senderHex].Balance.Cmp(*instructions.Transfer.Amount) < 0 {
				utils.LogError("ProcessRequest: insufficient balance for transfer")
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

			// Record transfer in history
			currentState.TransferHistory = append(currentState.TransferHistory, TransferRecord{
				From:      sender,
				To:        instructions.Transfer.To,
				Amount:    instructions.Transfer.Amount,
				Timestamp: time.Now(),
			})

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
				utils.LogError("ProcessRequest: failed to serialize event data: %v", err)
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
				utils.LogError("ProcessRequest: failed to serialize recipient event data: %v", err)
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
				utils.LogError("ProcessRequest: withdraw instruction is missing in payload")
				return ProcessResult{Error: "Withdraw instruction is missing in payload"}
			}
			if instructions.Withdraw.Amount == nil {
				return ProcessResult{Error: "Withdraw amount is nil"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				utils.LogError("ProcessRequest: account %s does not exist", senderHex)
				return ProcessResult{Error: fmt.Sprintf("Account %s does not exist", senderHex)}
			}

			if currentState.Accounts[senderHex].Balance.Cmp(*instructions.Withdraw.Amount) < 0 {
				utils.LogError("ProcessRequest: insufficient balance for account %s", senderHex)
				return ProcessResult{Error: fmt.Sprintf("Insufficient balance %s for withdrawal %s for account %s",
					currentState.Accounts[senderHex].Balance, *instructions.Withdraw.Amount, senderHex)}
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
				utils.LogError("ProcessRequest: failed to serialize withdraw event data: %v", err)
				return ProcessResult{Error: fmt.Sprintf("Failed to serialize withdraw event data: %+v, err: %v", withdrawEventData, err)}
			}

			events = append(events, PlainEvent{
				UserID:       sender,
				EventSubType: "withdrawal",
				Data:         withdrawEventDataBytes,
			})

		default:
			utils.LogError("ProcessRequest: unsupported instruction type: %s", instructions.Type)
			return ProcessResult{Error: fmt.Sprintf("Unsupported instruction type: [%s]", instructions.Type)}
		}
	}

	// Serialize the updated state
	newStateBytes, err := json.Marshal(currentState)
	if err != nil {
		utils.LogError("ProcessRequest: failed to serialize new state: %v", err)
		return ProcessResult{Error: fmt.Sprintf("Failed to serialize new state: %v", err)}
	}
	fuel := NewUint256(50)
	utils.LogDebug("ProcessRequest: sender=%s, eventsCount=%d, withdrawalsCount=%d, stateSize=%d, fuel=%v",
		senderHex, len(events), len(withdrawals), len(newStateBytes), fuel)
	return ProcessResult{
		State:       newStateBytes,
		Events:      events,
		Withdrawals: withdrawals,
		Fuel:        fuel,
	}
}

func GenerateDeanonymizationReport(payloadJSON, stateJSON string) DeanonymizationResult {
	// Deserialize payload
	var payload ReportPayloadInstructions
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		utils.LogError("GenerateDeanonymizationReport: failed to parse payload: %v", err)
		return DeanonymizationResult{Error: fmt.Sprintf("Failed to parse payload: %s, err: %v", payloadJSON, err)}
	}

	// Deserialize current state
	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		utils.LogError("GenerateDeanonymizationReport: failed to parse application state: %v", err)
		return DeanonymizationResult{Error: fmt.Sprintf("Failed to parse application state: %v", err)}
	}

	// Filter transfer history if account filter is specified
	transferHistory := currentState.TransferHistory
	if len(payload.AccountFilter) > 0 {
		filterSet := make(map[Address]struct{}, len(payload.AccountFilter))
		for _, addr := range payload.AccountFilter {
			filterSet[addr] = struct{}{}
		}
		var filtered []TransferRecord
		for _, tr := range transferHistory {
			_, fromMatch := filterSet[tr.From]
			_, toMatch := filterSet[tr.To]
			if fromMatch || toMatch {
				filtered = append(filtered, tr)
			}
		}
		transferHistory = filtered
	}

	// Create deanonymization report
	report := UnencryptedDeanonymizationReportData{
		Accounts:        currentState.Accounts,
		Nonce:           currentState.Nonce,
		TransferHistory: transferHistory,
	}

	// Serialize the report
	reportBytes, err := json.Marshal(report)
	if err != nil {
		utils.LogError("GenerateDeanonymizationReport: failed to serialize report: %v", err)
		return DeanonymizationResult{Error: fmt.Sprintf("Failed to serialize deanonymization report: %v", err)}
	}
	fuel := NewUint256(20)
	utils.LogDebug("GenerateDeanonymizationReport: accountsCount=%d, reportSize=%d, fuel=%v",
		len(currentState.Accounts), len(reportBytes), fuel)
	return DeanonymizationResult{Report: reportBytes, Fuel: fuel}
}

func GetAllocatedMemoryStats() MemoryStats {
	map_size, total_bytes := utils.GetAllocatedMemoryStats()
	return MemoryStats{
		MapSize:              map_size,
		CumulativeMemorySize: total_bytes,
	}
}
