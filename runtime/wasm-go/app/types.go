package app

import (
	"github.com/horizen-cce-common-go/wasm/types"
)

// ----- module internal types

// AccountState represents the state of a user account
type AccountState struct {
	Address types.Address  `json:"address"`
	Balance *types.Uint256 `json:"balance"`
}

// ApplicationInternalState represents the internal state of the application
type ApplicationInternalState struct {
	AppID    int64                    `json:"appId"`
	Accounts map[string]*AccountState `json:"accounts"`
	Nonce    uint64                   `json:"nonce"`
}

// WithdrawInstruction represents instructions for withdrawing funds
type WithdrawInstruction struct {
	To     types.Address  `json:"to"`
	Amount *types.Uint256 `json:"amount"`
}

// TransferInstruction represents instructions for transferring funds
// IvoiceID is an optional field the sender can include to track the payment
type TransferInstruction struct {
	To        types.Address  `json:"to"`
	Amount    *types.Uint256 `json:"amount"`
	InvoiceID string         `json:"invoice_id,omitempty"`
}

// DeanonymizeInstruction represents optional instructions for deanonymization
type DeanonymizeInstruction struct {
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

// ReportPayloadInstructions represents a specific information on how to generate a report
// TODO - We can add the list of the accounts to be included in the report and a boolean specifying whether
// we can omit empty accounts
type ReportPayloadInstructions struct {
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

// WithdrawalEvent has the same structure as SenderEvent
type WithdrawalEvent SenderEvent
