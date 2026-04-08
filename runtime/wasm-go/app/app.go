package app

import (
	"encoding/json"
	"fmt"

	"github.com/HorizenOfficial/vela-common-go/wasm/types"
	"github.com/HorizenOfficial/vela-common-go/wasm/utils"
	"github.com/HorizenOfficial/vela/pkg/common"
)

// --- High-Level Application Logic ---

// zeroAddressHex is the hex representation of the zero address (ETH).
var zeroAddressHex = (types.Address{}).Hex()

// recordTransaction appends a transaction record to the state's transaction log.
// Must be called after state.Nonce++ so the nonce matches the corresponding event.
func recordTransaction(state *ApplicationInternalState, txType string, from, to, tokenAddress types.Address, amount *types.Uint256, invoiceID string) {
	amountCopy := *amount
	state.Transactions = append(state.Transactions, TransactionRecord{
		Type:         txType,
		From:         from,
		To:           to,
		TokenAddress: tokenAddress,
		Amount:       &amountCopy,
		Nonce:        state.Nonce,
		Timestamp:    Now(),
		InvoiceID:    invoiceID,
	})
	if len(state.Transactions) > MaxTransactions {
		state.Transactions = state.Transactions[len(state.Transactions)-MaxTransactions:]
	}
}

// Deploy initializes the application state from constructor parameters.
// The AllowedTokens list specifies which tokens the app accepts; ETH (0x0) is always allowed.
func Deploy(appId int64, paramsJSON string) types.DeployResult {
	allowedTokens := make(map[string]bool)
	// ETH is always allowed
	allowedTokens[zeroAddressHex] = true

	if paramsJSON != "" {
		var params DeployParams
		if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
			utils.LogError("Deploy: failed to parse deploy params: %v", err)
			return types.DeployResult{
				Error: fmt.Sprintf("failed to parse deploy params: %v", err),
			}
		}
		for _, tokenHex := range params.AllowedTokens {
			if _, err := types.HexToAddress(tokenHex); err != nil {
				utils.LogError("Deploy: invalid token address %q: %v", tokenHex, err)
				return types.DeployResult{
					Error: fmt.Sprintf("invalid token address %q: %v", tokenHex, err),
				}
			}
			allowedTokens[tokenHex] = true
		}
	}

	initialState := &ApplicationInternalState{
		AppID:         uint64(appId),
		Accounts:      make(map[string]*AccountState),
		AllowedTokens: allowedTokens,
	}
	stateJSON, err := json.Marshal(initialState)
	if err != nil {
		utils.LogError("Deploy: failed to marshal initial state: %v", err)
		return types.DeployResult{
			Error: fmt.Sprintf("failed to marshal initial state: %v", err),
		}
	}
	fuel := types.NewUint256(5)
	utils.LogDebug("Deploy: appId=%d, allowedTokens=%d, stateSize=%d, fuel=%v", appId, len(allowedTokens), len(stateJSON), fuel)
	return types.DeployResult{
		State: stateJSON,
		Fuel:  fuel,
	}
}

// LoadModule is retained for cache warm-up by getOrLoadModule (see wasmtime_runtime.go).
// New deployments should use Deploy instead.
func LoadModule(appId int64) types.LoadModuleResult {
	initialState := &ApplicationInternalState{
		AppID:         uint64(appId),
		Accounts:      make(map[string]*AccountState),
		AllowedTokens: map[string]bool{zeroAddressHex: true},
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

// getOrCreateTokenBalance returns the balance for a specific token, initialising
// the map entry to zero if it does not yet exist.  The returned pointer is always
// stored in acc.Balances, so mutations are reflected in the state.
func getOrCreateTokenBalance(acc *AccountState, tokenHex string) *types.Uint256 {
	if acc.Balances == nil {
		acc.Balances = make(map[string]*types.Uint256)
	}
	if bal, ok := acc.Balances[tokenHex]; ok {
		return bal
	}
	bal := types.NewUint256(0)
	acc.Balances[tokenHex] = bal
	return bal
}

// resolveTokenHex returns the hex of the token address, defaulting to ETH if the address is zero.
func resolveTokenHex(tokenAddress types.Address) string {
	if tokenAddress == (types.Address{}) {
		return zeroAddressHex
	}
	return tokenAddress.Hex()
}

func DepositFunds(senderPtr *types.Address, tokenPtr *types.Address, value *types.Uint256, stateJSON string) types.DepositResult {
	if senderPtr == nil {
		utils.LogError("DepositFunds: sender address is nil")
		return types.DepositResult{Error: "Sender address is nil"}
	}

	if tokenPtr == nil {
		utils.LogError("DepositFunds: token address is nil")
		return types.DepositResult{Error: "Token address is nil"}
	}

	if value == nil {
		utils.LogError("DepositFunds: value is nil")
		return types.DepositResult{Error: "value is nil"}
	}

	var currentState ApplicationInternalState
	if err := json.Unmarshal([]byte(stateJSON), &currentState); err != nil {
		utils.LogError("DepositFunds: failed to parse application state: %v", err)
		return types.DepositResult{Error: fmt.Sprintf("Failed to parse application state: %v", err)}
	}

	tokenHex := resolveTokenHex(*tokenPtr)

	// Validate token against app allowlist
	if !currentState.AllowedTokens[tokenHex] {
		return types.DepositResult{Error: fmt.Sprintf("Token %s is not allowed by this application", tokenHex)}
	}

	senderHex := senderPtr.Hex()

	var events []types.PlainEvent

	// Handle deposit only if value > 0
	if !value.IsZero() {
		// Ensure sender account exists
		acc, exists := currentState.Accounts[senderHex]
		if !exists {
			acc = &AccountState{
				Address:  *senderPtr,
				Balances: make(map[string]*types.Uint256),
			}
			currentState.Accounts[senderHex] = acc
		}
		// Get or initialize per-token balance
		balance := getOrCreateTokenBalance(acc, tokenHex)

		// Add deposit to per-token balance (overflow check)
		oldBalance := *balance
		if balance.AddOverflow(*balance, *value) {
			utils.LogError("DepositFunds: overflow while adding amount %s to balance %s for account %s", value.String(), oldBalance.String(), senderHex)
			*balance = oldBalance // revert
			return types.DepositResult{Error: fmt.Sprintf("Overflow while adding amount %s to balance: %s", value, oldBalance)}
		}

		currentState.Nonce++
		recordTransaction(&currentState, "deposit", *senderPtr, *senderPtr, *tokenPtr, value, "")

		// Create deposit event
		eventData := DepositEvent{
			Type:         "deposit",
			TokenAddress: *tokenPtr,
			Amount:       value,
			Balance:      balance,
			Nonce:        currentState.Nonce,
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

	fuel := types.NewUint256(35)
	utils.LogDebug("DepositFunds: sender=%s, token=%s, value=%s, eventsCount=%d, stateSize=%d, fuel=%s",
		senderHex, tokenHex, value.String(), len(events), len(newStateBytes), fuel.String())
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

			// Resolve token (defaults to ETH if omitted)
			tokenHex := resolveTokenHex(instructions.Transfer.TokenAddress)

			// Validate token against app allowlist
			if !currentState.AllowedTokens[tokenHex] {
				return types.ProcessResult{Error: fmt.Sprintf("Token %s is not allowed for transfer", tokenHex)}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				utils.LogError("ProcessRequest: account %s does not exist", senderHex)
				return types.ProcessResult{Error: fmt.Sprintf("Account %s does not exist!", senderHex)}
			}

			senderBalance := getOrCreateTokenBalance(currentState.Accounts[senderHex], tokenHex)
			if senderBalance.Cmp(*instructions.Transfer.Amount) < 0 {
				utils.LogError("ProcessRequest: insufficient balance for transfer")
				return types.ProcessResult{Error: "Insufficient balance for transfer"}
			}

			recipientHex := instructions.Transfer.To.Hex()

			// Ensure recipient account exists
			if currentState.Accounts[recipientHex] == nil {
				currentState.Accounts[recipientHex] = &AccountState{
					Address: instructions.Transfer.To,
				}
			}

			recipientBalance := getOrCreateTokenBalance(currentState.Accounts[recipientHex], tokenHex)

			// Execute transfer (save both balances for revert on overflow)
			oldSenderBalance := *senderBalance
			oldRecipientBalance := *recipientBalance

			senderBalance.Sub(*senderBalance, *instructions.Transfer.Amount)
			if recipientBalance.AddOverflow(*recipientBalance, *instructions.Transfer.Amount) {
				utils.LogError("ProcessRequest: overflow while adding transfer amount %s to recipient %s balance %s",
					instructions.Transfer.Amount.String(), recipientHex, oldRecipientBalance.String())
				// Revert both sender and recipient balances
				*senderBalance = oldSenderBalance
				*recipientBalance = oldRecipientBalance
				return types.ProcessResult{Error: fmt.Sprintf("Overflow while adding transfer amount %s to recipient balance: %s",
					instructions.Transfer.Amount, oldRecipientBalance)}
			}
			currentState.Nonce++
			recordTransaction(&currentState, "transfer", sender, instructions.Transfer.To, instructions.Transfer.TokenAddress, instructions.Transfer.Amount, instructions.Transfer.InvoiceID)

			// Create events for both parties
			senderEventData := SenderEvent{
				Type:         "transfer_sent",
				To:           instructions.Transfer.To,
				TokenAddress: instructions.Transfer.TokenAddress,
				Amount:       instructions.Transfer.Amount,
				Balance:      senderBalance,
				Nonce:        currentState.Nonce,
				InvoiceID:    instructions.Transfer.InvoiceID,
			}
			senderEventDataBytes, err := json.Marshal(senderEventData)
			if err != nil {
				utils.LogError("ProcessRequest: failed to serialize event data: %v", err)
				return types.ProcessResult{Error: "Failed to serialize sender event data"}
			}

			recipientEventData := RecipientEvent{
				Type:         "transfer_received",
				From:         sender,
				TokenAddress: instructions.Transfer.TokenAddress,
				Amount:       instructions.Transfer.Amount,
				Balance:      recipientBalance,
				Nonce:        currentState.Nonce,
				InvoiceID:    instructions.Transfer.InvoiceID,
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

			// Resolve token (defaults to ETH if omitted)
			tokenHex := resolveTokenHex(instructions.Withdraw.TokenAddress)

			// Validate token against app allowlist
			if !currentState.AllowedTokens[tokenHex] {
				return types.ProcessResult{Error: fmt.Sprintf("Token %s is not allowed for withdrawal", tokenHex)}
			}

			// Validate sender account exists and has sufficient balance
			if currentState.Accounts[senderHex] == nil {
				utils.LogError("ProcessRequest: account %s does not exist", senderHex)
				return types.ProcessResult{Error: fmt.Sprintf("Account %s does not exist", senderHex)}
			}

			senderBalance := getOrCreateTokenBalance(currentState.Accounts[senderHex], tokenHex)

			if senderBalance.Cmp(*instructions.Withdraw.Amount) < 0 {
				utils.LogError("ProcessRequest: insufficient balance for account %s", senderHex)
				return types.ProcessResult{Error: fmt.Sprintf("Insufficient balance %s for withdrawal %s for account %s",
					senderBalance, *instructions.Withdraw.Amount, senderHex)}
			}

			// Execute withdrawal — debit per-token balance
			senderBalance.Sub(*senderBalance, *instructions.Withdraw.Amount)
			currentState.Nonce++
			recordTransaction(&currentState, "withdrawal", sender, instructions.Withdraw.To, instructions.Withdraw.TokenAddress, instructions.Withdraw.Amount, "")

			// Create token-aware withdrawal
			withdrawals = append(withdrawals, types.Withdrawal{
				TokenAddress:       instructions.Withdraw.TokenAddress,
				DestinationAddress: instructions.Withdraw.To,
				Amount:             instructions.Withdraw.Amount,
			})

			// Create event for sender
			withdrawEventData := WithdrawalEvent{
				Type:         "withdrawal",
				To:           instructions.Withdraw.To,
				TokenAddress: instructions.Withdraw.TokenAddress,
				Amount:       instructions.Withdraw.Amount,
				Balance:      senderBalance,
				Nonce:        currentState.Nonce,
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
						break
					}
					filtered = append(filtered, tx)
				}

				// Look up current balances for the requested address
				var balances map[string]*types.Uint256
				if acc := currentState.Accounts[addrHex]; acc != nil && acc.Balances != nil {
					balances = acc.Balances
				} else {
					balances = make(map[string]*types.Uint256)
				}

				reportBytes, err = json.Marshal(TxHistoryReport{
					Address:      addr,
					Balances:     balances,
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
