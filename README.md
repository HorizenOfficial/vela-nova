# Horizen Vela Nova - Private Transfer Application

Privacy-preserving token transfer application for the Horizen Vela platform. Includes a WASM runtime for confidential transactions inside the TEE and a client-side wallet CLI.

[runtime/](runtime/) <br/>
Application runtime for Horizen Vela

[wallet/](wallet) <br/>
Client-side wallet CLI

## Deanonymization Reports

The application supports deanonymization reports for regulatory compliance. An authorized auditor can request a report that is generated inside the TEE, encrypted, and stored in the Authority Service.

### Report Types

#### Balances Snapshot (default)

Returns all addresses with their current balance.

```bash
novaw requestreport
```

Report output:
```json
{
  "accounts": {
    "0xabc...": { "address": "0xabc...", "balance": "0x1bc16d674ec80000" }
  },
  "nonce": 5
}
```

#### Transaction History

Given a specific address, returns all transactions involving it (as sender or receiver): deposits, transfers, and withdrawals.

```bash
novaw requestreport --report-type tx_history --address 0xabc...
```

Report output:
```json
{
  "address": "0xabc...",
  "transactions": [
    { "type": "deposit",    "from": "0xabc...", "to": "0xabc...", "amount": "0x...", "nonce": 1 },
    { "type": "transfer",   "from": "0xabc...", "to": "0xdef...", "amount": "0x...", "nonce": 2, "invoice_id": "INV-001" },
    { "type": "withdrawal", "from": "0xabc...", "to": "0x123...", "amount": "0x...", "nonce": 3 }
  ]
}
```

Transaction history is stored in the private state and grows with each operation. Every deposit, transfer, and withdrawal appends a record that is then queryable via this report type.