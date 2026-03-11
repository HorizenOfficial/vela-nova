package app

import (
	"time"

	"github.com/HorizenOfficial/vela-common-go/wasm/types"
)

// Now returns the current Unix timestamp. Defined as a variable so tests can override it.
var Now = func() int64 { return time.Now().Unix() }

const MaxInvoiceIDLength = 100
const MaxTransactions = 50

// ----- module internal types

// AccountState represents the state of a user account
type AccountState struct {
	Address types.Address  `json:"address"`
	Balance *types.Uint256 `json:"balance"`
}

// TransactionRecord represents a single transaction stored in the private state
type TransactionRecord struct {
	Type      string         `json:"type"` // "deposit", "transfer", "withdrawal"
	From      types.Address  `json:"from"`
	To        types.Address  `json:"to"`
	Amount    *types.Uint256 `json:"amount"`
	Nonce     uint64         `json:"nonce"`
	Timestamp int64          `json:"timestamp"`
	InvoiceID string         `json:"invoice_id,omitempty"`
}

// ApplicationInternalState represents the internal state of the application
type ApplicationInternalState struct {
	AppID        uint64                   `json:"appId"`
	Accounts     map[string]*AccountState `json:"accounts"`
	Nonce        uint64                   `json:"nonce"`
	Transactions []TransactionRecord      `json:"transactions,omitempty"`
}

// WithdrawInstruction represents instructions for withdrawing funds
type WithdrawInstruction struct {
	To     types.Address  `json:"to"`
	Amount *types.Uint256 `json:"amount"`
}

// TransferInstruction represents instructions for transferring funds
// InvoiceID is an optional field the sender can include to track the payment
type TransferInstruction struct {
	To        types.Address  `json:"to"`
	Amount    *types.Uint256 `json:"amount"`
	InvoiceID string         `json:"invoice_id,omitempty"`
}

// DeanonymizeInstruction represents optional instructions for deanonymization
type DeanonymizeInstruction struct {
	ReportType    string        `json:"report_type,omitempty"`    // "balances" (default) or "tx_history"
	Address       types.Address `json:"address,omitempty"`        // required for tx_history
	FromTimestamp int64         `json:"from_timestamp,omitempty"` // filter tx_history: start unix timestamp (inclusive)
	ToTimestamp   int64         `json:"to_timestamp,omitempty"`   // filter tx_history: end unix timestamp (inclusive)
}

// PayloadInstructions represents the deserialized payload instructions
type PayloadInstructions struct {
	Type        string                  `json:"type"`
	Transfer    *TransferInstruction    `json:"transfer,omitempty"`
	Withdraw    *WithdrawInstruction    `json:"withdraw,omitempty"`
	Deanonymize *DeanonymizeInstruction `json:"deanonymize,omitempty"`
}

// DeanonymizationReport represents the structure of the deanonymization report
type DeanonymizationReport struct {
	Accounts map[string]*AccountState `json:"accounts"`
	Nonce    uint64                   `json:"nonce"`
}

// TxHistoryReport is the report returned for a tx_history deanonymization request
type TxHistoryReport struct {
	Address      types.Address       `json:"address"`
	Balance      *types.Uint256      `json:"balance"`
	Transactions []TransactionRecord `json:"transactions"`
}

type DepositEvent struct {
	Type    string         `json:"type"`
	Amount  *types.Uint256 `json:"amount"`
	Balance *types.Uint256 `json:"balance"`
	Nonce   uint64         `json:"nonce"`
}

type SenderEvent struct {
	Type      string         `json:"type"`
	To        types.Address  `json:"to"`
	Amount    *types.Uint256 `json:"amount"`
	Balance   *types.Uint256 `json:"balance"`
	Nonce     uint64         `json:"nonce"`
	InvoiceID string         `json:"invoice_id,omitempty"`
}

type RecipientEvent struct {
	Type      string         `json:"type"`
	From      types.Address  `json:"from"`
	Amount    *types.Uint256 `json:"amount"`
	Balance   *types.Uint256 `json:"balance"`
	Nonce     uint64         `json:"nonce"`
	InvoiceID string         `json:"invoice_id,omitempty"`
}

type WithdrawalEvent struct {
	Type    string         `json:"type"`
	To      types.Address  `json:"to"`
	Amount  *types.Uint256 `json:"amount"`
	Balance *types.Uint256 `json:"balance"`
	Nonce   uint64         `json:"nonce"`
}
