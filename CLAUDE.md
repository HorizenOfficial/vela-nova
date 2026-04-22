# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository layout

This repo is a concrete application built on the **Vela** platform (privacy-preserving execution in a TEE). It contains two independent Go modules:

- `runtime/wasm-go/` — the **Payment App** compiled to WebAssembly with **TinyGo** and executed *inside* the TEE by the Vela Wasmtime runtime. Module: `github.com/HorizenOfficial/vela-nova/payment-app`.
- `wallet/` — the **`novaw`** client CLI (Cobra) that builds/encrypts `PayloadInstructions`, submits them on-chain, and fetches/decrypts reports from the Authority Service. Module: `github.com/HorizenOfficial/vela-nova/wallet`.

Both modules depend on `github.com/HorizenOfficial/vela` (framework + Wasmtime host) and `github.com/HorizenOfficial/vela-common-go` (shared wire types: `types.Address`, `types.Uint256`, result/event structs, memory helpers). Keep these two versions in sync across both `go.mod` files — the wallet serializes structures that the WASM module deserializes, so any type drift between them breaks the end-to-end flow. 


## Common commands

All `go` and `make` commands must be run **inside the respective module directory** (`wallet/` or `runtime/wasm-go/`) — there is no root `go.mod`.

### Wallet (`wallet/`)

```bash
go build -o novaw          # build the CLI
go test -v ./...           # run all wallet tests
go test -v -run TestName ./cmd   # run a single test
```

### WASM module (`runtime/wasm-go/`)

```bash
make build              # dev WASM → build/payment_app.wasm (tinygo -target=wasi)
make production_build   # optimized → production_build/payment_app.wasm (-opt=s -no-debug)
make wat                # disassemble to .wat (requires wasm2wat)
make clean

go test -v ./...                  # full suite (requires Wasmtime via the vela dep)
CI_FLAG=true go test -v ./...     # fast suite; skips Wasmtime-dependent tests and system_tests
go test -v -run TestName ./...    # single test
```

**TinyGo is required** for the WASM build (CI pins v0.39.0). The Go toolchain alone can compile/test the module but cannot produce the `.wasm` artifact.

### Test-file roles (wasm-go)

- `wasmtime_runtime_test.go` — exercises the `WasmtimeRuntime` interaction layer (error paths, concurrency, large payloads).
- `integration_test.go` — loads the compiled `payment_app.wasm` and validates business logic through real WASM calls.
- `system_tests/payment_app_system_test.go` — end-to-end with simulated Executor + Manager + DB + blockchain; skipped under `CI_FLAG=true` because it's slow.

## Architecture

### Execution pipeline

```
User → novaw CLI → EVM contract (Processor) → Manager → Executor → Wasmtime (TEE) → payment_app.wasm
```

The Vela framework is **application-agnostic**: it never parses payloads. This repo is the only layer that knows about payments. The WASM module is the trust boundary — all plaintext balances, transaction history, and business logic live *only* inside the TEE.

### WASM exports (`runtime/wasm-go/main.go`)

The module exposes exactly these functions to the host runtime:

| Export | Purpose |
|---|---|
| `deploy(appId, paramsJSON)` | Initialize state from `DeployParams` (`AllowedTokens`) — ETH (0x0) always allowed |
| `load_module(appId)` | Re-hydrate state on runtime start |
| `deposit(appId, sender, token, value, state)` | Credit a token balance |
| `process_request(appId, sender, requestType, payload, state)` | Dispatch transfer / withdraw / deanonymize based on `PayloadInstructions.Type` |
| `get_memory_stats()` | WASM memory diagnostics |

`main.go` is a thin bridge: it converts pointers via `vela-common-go/wasm/types` + `utils`, calls into `app/`, and serializes the result. Real logic lives in `app/app.go`; schemas in `app/types.go`.

### Shared state & events

`ApplicationInternalState` (in `app/types.go`) holds `Accounts` (per-token balances), `AllowedTokens`, a monotonic `Nonce`, and a bounded transaction log (`MaxTransactions = 50`). `recordTransaction` must be called **after** `state.Nonce++` so each `TransactionRecord` nonce matches its emitted event's nonce — this is the invariant that ties the private ledger to on-chain events.

Event payloads (`DepositEvent`, `SenderEvent`, `RecipientEvent`, `WithdrawalEvent`) carry post-op balances and are produced by the WASM module for the Executor to relay on-chain.

### Deanonymization reports

Regulatory escape hatch. An authorized auditor calls `novaw requestreport [--report-type tx_history --address 0x…]`; the TEE generates and encrypts a `DeanonymizationReport` (balances snapshot) or `TxHistoryReport` and stores it via the Authority Service, which `novaw downloadreport` + `decryptreport` retrieves. `tx_history` supports `--from_timestamp` / `--to_timestamp` filtering against the bounded tx log.

### Wallet internals (`wallet/app`)

`Config` (loaded from `wallet.conf` via `magiconair/properties`) carries the keypair, RPC endpoints (`rpcUrl`, `ProcessorAddress`, `TeeAuthenticatorAddress`, `AuthorityServiceURL`, `SubgraphURL`), the on-chain `ApplicationID`, polling settings, and an optional ERC-20 `TokenRegistry` populated from `token.<SYMBOL>.address` / `token.<SYMBOL>.decimals` keys. `ErrPollingTimeout` means "subgraph didn't see the event before timeout" — the on-chain request may still succeed; do not treat it as a hard failure.

Deploy note: the sender used by `novaw deployapp` must have `DEPLOYER_ROLE` on the `ProcessorEndpoint` contract.

## CI / release

`.github/workflows/ci.yml` runs the wasm-go suite twice (fast + full), then the wallet suite, using Go 1.24 and TinyGo 0.39.0. Tags matching `v*` trigger a release that ships `production_build/payment_app.wasm`, `wallet/releases/novaw-linux`, and `wallet.conf.template` as GitHub Release artifacts. Bumping the Vela dependency version typically requires tagging both `vela` and `vela-common-go` at matching versions and updating both `go.mod` files together.
