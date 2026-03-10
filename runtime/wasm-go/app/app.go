package app

import (
	"encoding/json"
	"fmt"

	"github.com/HorizenOfficial/vela-common-go/wasm/types"
	"github.com/HorizenOfficial/vela-common-go/wasm/utils"
	"github.com/HorizenOfficial/vela/pkg/common"
)

// --- High-Level Application Logic ---

// recordTransaction appends a transaction record to the state's transaction log.
// Must be called after state.Nonce++ so the nonce matches the corresponding event.
func recordTransaction(state *ApplicationInternalState, txType string, from, to types.Address, amount *types.Uint256, invoiceID string) {
	amountCopy := *amount
	state.Transactions = append(state.Transactions, TransactionRecord{
		Type:      txType,
		From:      from,
		To:        to,
		Amount:    &amountCopy,
		Nonce:     state.Nonce,
		Timestamp: Now(),
		InvoiceID: invoiceID,
	})
}

func LoadModule(appId int64) types.LoadModuleResult {
	initialState := &ApplicationInternalState{
		AppID:    uint64(appId),
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
		recordTransaction(&currentState, "deposit", *senderPtr, *senderPtr, value, "")

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

func ProcessRequest(senderPtr *types.Address, requestType int32, payloadJSON, stateJSON string) types.ProcessResult {
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

	// Determine instruction type: requestType has priority over payload
	var instructionType string
	var instructions PayloadInstructions

	// Parse payload if present (for additional options like deanonymize params)
	if payloadJSON != "" && payloadJSON != "{}" {
		if err := json.Unmarshal([]byte(payloadJSON), &instructions); err != nil {
			utils.LogError("ProcessRequest: failed to parse payload instructions: %v", err)
			return types.ProcessResult{Error: fmt.Sprintf("Failed to parse payload instructions: %v", err)}
		}
	}

	// requestType takes precedence over payload type
	if requestType == int32(common.Deanonymize) {
		instructionType = "deanonymize"
	} else {
		instructionType = instructions.Type
	}

	if instructionType != "" {
		switch instructionType {
		case "transfer":
			if instructions.Transfer == nil {
				utils.LogError("ProcessRequest: transfer instruction is missing in payload")
				return types.ProcessResult{Error: "Transfer instruction is missing"}
			}
			if instructions.Transfer.Amount == nil {
				utils.LogError("ProcessRequest: transfer amount is nil")
				return types.ProcessResult{Error: "Transfer amount is nil"}
			}
			if len(instructions.Transfer.InvoiceID) > MaxInvoiceIDLength {
				utils.LogError("ProcessRequest: invoice_id exceeds maximum length of %d characters", MaxInvoiceIDLength)
				return types.ProcessResult{Error: fmt.Sprintf("invoice_id exceeds maximum length of %d characters", MaxInvoiceIDLength)}
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

			// Execute transfer (save both balances for revert on overflow)
			oldSenderBalance := *currentState.Accounts[senderHex].Balance
			oldRecipientBalance := *currentState.Accounts[recipientHex].Balance

			currentState.Accounts[senderHex].Balance.Sub(*currentState.Accounts[senderHex].Balance, *instructions.Transfer.Amount)
			if currentState.Accounts[recipientHex].Balance.AddOverflow(*currentState.Accounts[recipientHex].Balance, *instructions.Transfer.Amount) {
				utils.LogError("ProcessRequest: overflow while adding transfer amount %s to recipient %s balance %s",
					instructions.Transfer.Amount.String(), recipientHex, oldRecipientBalance.String())
				// Revert both sender and recipient balances
				*currentState.Accounts[senderHex].Balance = oldSenderBalance
				*currentState.Accounts[recipientHex].Balance = oldRecipientBalance
				return types.ProcessResult{Error: fmt.Sprintf("Overflow while adding transfer amount %s to recipient balance: %s",
					instructions.Transfer.Amount, oldRecipientBalance)}
			}
			currentState.Nonce++
			recordTransaction(&currentState, "transfer", sender, instructions.Transfer.To, instructions.Transfer.Amount, instructions.Transfer.InvoiceID)

			// Create events for both parties
			senderEventData := SenderEvent{
				Type:      "transfer_sent",
				To:        instructions.Transfer.To,
				Amount:    instructions.Transfer.Amount,
				Balance:   currentState.Accounts[senderHex].Balance,
				Nonce:     currentState.Nonce,
				InvoiceID: instructions.Transfer.InvoiceID,
			}
			senderEventDataBytes, err := json.Marshal(senderEventData)
			if err != nil {
				utils.LogError("ProcessRequest: failed to serialize event data: %v", err)
				return types.ProcessResult{Error: "Failed to serialize sender event data"}
			}

			recipientEventData := RecipientEvent{
				Type:      "transfer_received",
				From:      sender,
				Amount:    instructions.Transfer.Amount,
				Balance:   currentState.Accounts[recipientHex].Balance,
				Nonce:     currentState.Nonce,
				InvoiceID: instructions.Transfer.InvoiceID,
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
			recordTransaction(&currentState, "withdrawal", sender, instructions.Withdraw.To, instructions.Withdraw.Amount, "")

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

		case "deanonymize":
			reportType := "balances"
			if instructions.Deanonymize != nil && instructions.Deanonymize.ReportType != "" {
				reportType = instructions.Deanonymize.ReportType
			}

			var reportBytes []byte
			var err error

			switch reportType {
			case "balances":
				reportBytes, err = json.Marshal(DeanonymizationReport{
					Accounts: currentState.Accounts,
					Nonce:    currentState.Nonce,
				})
			case "tx_history":
				if instructions.Deanonymize.Address.IsZero() {
					return types.ProcessResult{Error: "tx_history report requires a non-zero address"}
				}
				addr := instructions.Deanonymize.Address
				addrHex := addr.Hex()
				fromTs := instructions.Deanonymize.FromTimestamp
				toTs := instructions.Deanonymize.ToTimestamp

				filtered := []TransactionRecord{}
				for _, tx := range currentState.Transactions {
					if tx.From != addr && tx.To != addr {
						continue
					}
					if fromTs > 0 && tx.Timestamp < fromTs {
						continue
					}
					if toTs > 0 && tx.Timestamp > toTs {
						continue
					}
					filtered = append(filtered, tx)
				}

				// Look up current balance for the requested address
				var balance *types.Uint256
				if acc := currentState.Accounts[addrHex]; acc != nil {
					balance = acc.Balance
				} else {
					balance = types.NewUint256(0)
				}

				reportBytes, err = json.Marshal(TxHistoryReport{
					Address:      addr,
					Balance:      balance,
					Transactions: filtered,
				})
			default:
				return types.ProcessResult{Error: fmt.Sprintf("Unsupported report type: %s", reportType)}
			}

			if err != nil {
				utils.LogError("ProcessRequest: failed to serialize %s report: %v", reportType, err)
				return types.ProcessResult{Error: fmt.Sprintf("Failed to serialize %s report: %v", reportType, err)}
			}

			utils.LogDebug("ProcessRequest: deanonymize sender=%s, reportType=%s, reportSize=%d",
				senderHex, reportType, len(reportBytes))
			return types.ProcessResult{
				State:  []byte(stateJSON),
				Report: reportBytes,
				Fuel:   types.NewUint256(20),
			}

		default:
			utils.LogError("ProcessRequest: unsupported instruction type: %s", instructionType)
			return types.ProcessResult{Error: fmt.Sprintf("Unsupported instruction type: [%s]", instructionType)}
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

func GetAllocatedMemoryStats() types.MemoryStats {
	map_size, total_bytes := utils.GetAllocatedMemoryStats()
	return types.MemoryStats{
		MapSize:              map_size,
		CumulativeMemorySize: total_bytes,
	}
}
