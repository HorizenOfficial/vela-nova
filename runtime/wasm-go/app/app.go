package app

import (
	"encoding/json"
	"fmt"

	"github.com/horizen-cce-common-go/wasm/types"
	"github.com/horizen-cce-common-go/wasm/utils"
)

// --- High-Level Application Logic ---

func LoadModule(appId int64) types.LoadModuleResult {
	initialState := &ApplicationInternalState{
		AppID:    appId,
		Accounts: make(map[string]*AccountState),
	}
	stateJSON, err := json.Marshal(initialState)
	if err != nil {
		utils.LogError("LoadModule: failed to marshal initial state: %v", err)
		return types.LoadModuleResult{
			Error: fmt.Sprintf("failed to marshal initial state: %v", err),
		}
	}
	fuel := types.NewUint256(5)
	utils.LogDebug("LoadModule: appId=%d, stateSize=%d, fuel=%v", appId, len(stateJSON), fuel)
	return types.LoadModuleResult{
		State: stateJSON,
		Fuel:  fuel,
	}
}

func DepositFunds(senderPtr *types.Address, value *types.Uint256, stateJSON string) types.DepositResult {
	if senderPtr == nil {
		utils.LogError("DepositFunds: sender address is nil")
		return types.DepositResult{Error: "Sender address is nil"}
	}

	//This should never happens but just in case
	if value == nil {
		utils.LogError("DepositFunds: value is nil")
		return types.DepositResult{Error: "value is nil"}
	}

	utils.LogDebug("DepositFunds called with address %s, value %s", senderPtr.String(), value.String())

	senderHex := senderPtr.Hex()

	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		// we could add the stateJSON to the error, but it is not safe if it is very large
		utils.LogError("DepositFunds: failed to parse application state: %v", err)
		return types.DepositResult{Error: fmt.Sprintf("Failed to parse application state: %v", err)}
	}

	var events []types.PlainEvent

	// Handle deposit only if value > 0
	if !value.IsZero() {
		// Ensure sender account exists
		if currentState.Accounts[senderHex] == nil {
			currentState.Accounts[senderHex] = &AccountState{
				Address: *senderPtr,
				Balance: types.NewUint256(0),
			}
		}

		// Add deposit to sender's balance (copy old balance by value for revert on overflow)
		oldBalance := *currentState.Accounts[senderHex].Balance
		if currentState.Accounts[senderHex].Balance.AddOverflow(*currentState.Accounts[senderHex].Balance, *value) {
			utils.LogError("DepositFunds: overflow while adding amount %s to balance %s for account %s", value.String(), oldBalance.String(), senderHex)
			*currentState.Accounts[senderHex].Balance = oldBalance // revert to previous balance
			return types.DepositResult{Error: fmt.Sprintf("Overflow while adding amount %s to balance: %s", value, oldBalance)}
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
			return types.DepositResult{Error: fmt.Sprintf("Failed to serialize event data: %+v, err: %v", eventData, err)}
		}

		events = append(events, types.PlainEvent{
			UserID:       *senderPtr,
			EventSubType: "deposit",
			Data:         eventDataBytes,
		})
	}

	// Serialize the updated state
	newStateBytes, err := json.Marshal(&currentState)
	if err != nil {
		utils.LogError("DepositFunds: failed to serialize new state: %v", err)
		return types.DepositResult{Error: fmt.Sprintf("Failed to serialize new state: %v", err)}
	}

	// Get balance string safely (account may not exist, for instance in case of zero deposits)
	var balanceStr string
	if currentState.Accounts[senderHex] != nil {
		balanceStr = currentState.Accounts[senderHex].Balance.String()
	} else {
		balanceStr = "0"
	}

	fuel := types.NewUint256(35)
	utils.LogDebug("DepositFunds: sender=%s, value=%s, newBalance=%s, eventsCount=%d, stateSize=%d, fuel=%s",
		senderHex, value.String(), balanceStr, len(events), len(newStateBytes), fuel.String())
	return types.DepositResult{State: newStateBytes, Events: events, Fuel: fuel}
}

func ProcessRequest(senderPtr *types.Address, payloadJSON, stateJSON string) types.ProcessResult {
	if senderPtr == nil {
		return types.ProcessResult{Error: "Sender address is missing"}
	}

	sender := *senderPtr
	senderHex := sender.Hex()

	// Deserialize current state
	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		utils.LogError("ProcessRequest: failed to parse application state: %v", err)
		return types.ProcessResult{Error: fmt.Sprintf("Failed to parse application state: %v", err)}
	}

	var events []types.PlainEvent
	var withdrawals []types.Withdrawal

	// Process payload instructions if payload is not empty
	if payloadJSON != "" {
		var instructions PayloadInstructions
		if err := json.Unmarshal([]byte(payloadJSON), &instructions); err != nil {
			utils.LogError("ProcessRequest: failed to parse payload instructions: %v", err)
			return types.ProcessResult{Error: fmt.Sprintf("Failed to parse payload instructions: %v", err)}
		}

		switch instructions.Type {
		case "transfer":
			if instructions.Transfer == nil {
				utils.LogError("ProcessRequest: transfer instruction is missing in payload")
				return types.ProcessResult{Error: "Transfer instruction is missing"}
			}
			if instructions.Transfer.Amount == nil {
				utils.LogError("ProcessRequest: transfer amount is nil")
				return types.ProcessResult{Error: "Transfer amount is nil"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				utils.LogError("ProcessRequest: account %s does not exist", senderHex)
				return types.ProcessResult{Error: fmt.Sprintf("Account %s does not exist!", senderHex)}
			}
			if currentState.Accounts[senderHex].Balance.Cmp(*instructions.Transfer.Amount) < 0 {
				utils.LogError("ProcessRequest: insufficient balance for transfer")
				return types.ProcessResult{Error: "Insufficient balance for transfer"}
			}

			recipientHex := instructions.Transfer.To.Hex()

			// Ensure recipient account exists
			if currentState.Accounts[recipientHex] == nil {
				currentState.Accounts[recipientHex] = &AccountState{
					Address: instructions.Transfer.To,
					Balance: types.NewUint256(0),
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
				utils.LogError("ProcessRequest: failed to serialize event data: %v", err)
				return types.ProcessResult{Error: "Failed to serialize sender event data"}
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
				return types.ProcessResult{Error: "Failed to serialize recipient event data"}
			}

			events = append(events, types.PlainEvent{
				UserID:       sender,
				EventSubType: "transfer_sent",
				Data:         senderEventDataBytes,
			})

			events = append(events, types.PlainEvent{
				UserID:       instructions.Transfer.To,
				EventSubType: "transfer_received",
				Data:         recipientEventDataBytes,
			})

		case "withdraw":
			if instructions.Withdraw == nil {
				utils.LogError("ProcessRequest: withdraw instruction is missing in payload")
				return types.ProcessResult{Error: "Withdraw instruction is missing in payload"}
			}
			if instructions.Withdraw.Amount == nil {
				return types.ProcessResult{Error: "Withdraw amount is nil"}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				utils.LogError("ProcessRequest: account %s does not exist", senderHex)
				return types.ProcessResult{Error: fmt.Sprintf("Account %s does not exist", senderHex)}
			}

			if currentState.Accounts[senderHex].Balance.Cmp(*instructions.Withdraw.Amount) < 0 {
				utils.LogError("ProcessRequest: insufficient balance for account %s", senderHex)
				return types.ProcessResult{Error: fmt.Sprintf("Insufficient balance %s for withdrawal %s for account %s",
					currentState.Accounts[senderHex].Balance, *instructions.Withdraw.Amount, senderHex)}
			}

			// Execute withdrawal
			currentState.Accounts[senderHex].Balance.Sub(*currentState.Accounts[senderHex].Balance, *instructions.Withdraw.Amount)
			currentState.Nonce++

			// Create withdrawal
			withdrawals = append(withdrawals, types.Withdrawal{
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
				return types.ProcessResult{Error: fmt.Sprintf("Failed to serialize withdraw event data: %+v, err: %v", withdrawEventData, err)}
			}

			events = append(events, types.PlainEvent{
				UserID:       sender,
				EventSubType: "withdrawal",
				Data:         withdrawEventDataBytes,
			})

		default:
			utils.LogError("ProcessRequest: unsupported instruction type: %s", instructions.Type)
			return types.ProcessResult{Error: fmt.Sprintf("Unsupported instruction type: [%s]", instructions.Type)}
		}
	}

	// Serialize the updated state
	newStateBytes, err := json.Marshal(currentState)
	if err != nil {
		utils.LogError("ProcessRequest: failed to serialize new state: %v", err)
		return types.ProcessResult{Error: fmt.Sprintf("Failed to serialize new state: %v", err)}
	}
	fuel := types.NewUint256(50)
	utils.LogDebug("ProcessRequest: sender=%s, eventsCount=%d, withdrawalsCount=%d, stateSize=%d, fuel=%v",
		senderHex, len(events), len(withdrawals), len(newStateBytes), fuel)
	return types.ProcessResult{
		State:       newStateBytes,
		Events:      events,
		Withdrawals: withdrawals,
		Fuel:        fuel,
	}
}

func GenerateDeanonymizationReport(payloadJSON, stateJSON string) types.DeanonymizationResult {
	// Deserialize payload
	var payload ReportPayloadInstructions
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		utils.LogError("GenerateDeanonymizationReport: failed to parse payload: %v", err)
		return types.DeanonymizationResult{Error: fmt.Sprintf("Failed to parse payload: %s, err: %v", payloadJSON, err)}
	}

	// Deserialize current state
	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		utils.LogError("GenerateDeanonymizationReport: failed to parse application state: %v", err)
		return types.DeanonymizationResult{Error: fmt.Sprintf("Failed to parse application state: %v", err)}
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
		utils.LogError("GenerateDeanonymizationReport: failed to serialize report: %v", err)
		return types.DeanonymizationResult{Error: fmt.Sprintf("Failed to serialize deanonymization report: %v", err)}
	}
	fuel := types.NewUint256(20)
	utils.LogDebug("GenerateDeanonymizationReport: accountsCount=%d, reportSize=%d, fuel=%v",
		len(currentState.Accounts), len(reportBytes), fuel)
	return types.DeanonymizationResult{Report: reportBytes, Fuel: fuel}
}

func GetAllocatedMemoryStats() types.MemoryStats {
	map_size, total_bytes := utils.GetAllocatedMemoryStats()
	return types.MemoryStats{
		MapSize:              map_size,
		CumulativeMemorySize: total_bytes,
	}
}
