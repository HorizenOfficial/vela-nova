package app

import (
	"github.com/horizen-cce-common-go/wasm/types"
)

const MaxInvoiceIDLength = 100

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
	ReportType string        `json:"report_type,omitempty"` // "balances" (default) or "tx_history"
	Address    types.Address `json:"address,omitempty"`     // required for tx_history
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
