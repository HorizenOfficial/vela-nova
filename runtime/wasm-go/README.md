# WASM Go Module: Payment App

This module contains the Go implementation of the **Payment App** WASM module — a privacy-preserving payment application for deposits, transfers, and withdrawals, built on the Vela (Privacy Preserving Execution System) framework.

The Vela framework (`vela`) is **application-agnostic**: it provides a generic execution pipeline (EVM blockchain → Manager → Executor → WASM Runtime) that processes requests without ever parsing application payloads. This module is a specific application that plugs into that framework — the only layer that knows about payment logic. Any WASM module implementing the expected exports can replace it.

**Note:** The WebAssembly (WASM) runtime itself (Wasmtime) is implemented and maintained in the `vela` repository. This module depends on that runtime for building and executing tests.

## Prerequisites

### Install TinyGo

Before building the WASM module, you need to install TinyGo.

**macOS (using Homebrew):**
```bash
brew tap tinygo-org/tools
brew install tinygo
```

**Linux/Manual Installation:**
```bash
wget https://github.com/tinygo-org/tinygo/releases/download/v0.39.0/tinygo_0.39.0_amd64.deb
sudo dpkg -i tinygo_0.39.0_amd64.deb
```
*Note: Check the [TinyGo releases page](https://github.com/tinygo-org/tinygo/releases) for the latest version.*

**Verify Installation:**
```bash
tinygo version
```

## Dependencies

This module depends on two external packages:

- **`vela`** — The application-agnostic Vela framework. Provides the generic WASM runtime (Wasmtime), common types (`common.Request`, `common.Event`, `common.Withdrawal`), and the `Runtime` interface. Used in tests to run the compiled WASM module.
- **`vela-common-go/wasm`** — Shared WASM guest-side types and utilities. Provides `types.Uint256`, `types.Address`, `types.PlainEvent`, result types (`LoadModuleResult`, `DepositResult`, `ProcessResult`, `DeanonymizationResult`), memory allocator (`utils.Allocate`/`Deallocate`), logging, and pointer conversions. These are the shared data structures that the wallet also imports to ensure identical serialization.

Both dependencies use `replace` directives in `go.mod`. For local development, uncomment the local path replaces pointing to sibling directories.

## Building

To build the WASM module, you can use the provided Makefile:

```bash
make build
```

This command uses TinyGo to compile the `main.go` file into a WASM module. The underlying command is:
```bash
tinygo build -o build/payment_app.wasm -target wasi main.go
```

This will create the `build/payment_app.wasm` file.

## Module Structure

```
runtime/wasm-go/
├── main.go              # WASM export functions (bridge between runtime and app logic)
├── app/
│   ├── app.go           # Application logic (LoadModule, DepositFunds, ProcessRequest, GenerateDeanonymizationReport)
│   └── types.go         # App-specific types (PayloadInstructions, TransferInstruction, WithdrawInstruction, AccountState)
├── wasmtime_runtime_test.go  # Unit/integration tests against WASM runtime
├── integration_test.go       # Integration tests for compiled WASM binary
├── system_tests/             # E2E system tests (full Vela stack simulation)
├── build/                    # Dev WASM binary output
├── production_build/         # Production WASM binary output
└── Makefile
```

### WASM Exports

The module exports these functions for the generic Vela runtime to call:

| Export | Purpose |
|---|---|
| `load_module(appId)` | Initialize application state |
| `deposit(appId, sender, value, state)` | Credit sender account |
| `process_request(appId, sender, payload, state)` | Handle transfers and withdrawals |
| `generate_deanonymization_report(payload, state)` | Generate compliance reports |
| `get_memory_stats()` | Return WASM memory allocation statistics |

### Shared Data Structures

The wallet (`wallet/`) constructs `PayloadInstructions` (defined in `app/types.go`) and encrypts them before submitting to the blockchain. This module receives and decrypts those instructions inside the TEE. Both sides import `types.Address` and `types.Uint256` from `vela-common-go/wasm/types` to ensure identical serialization.

## Development Workflow

1.  **Modify WASM Module**: The core application logic is in `app/app.go`. The WASM export bridge is in `main.go`. App-specific types are in `app/types.go`. Shared guest-side types and utilities come from `vela-common-go/wasm`.
2.  **Rebuild Module**: After making changes, rebuild the WASM module using `make build` or the `tinygo` command directly.
3.  **Update Tests**: Add or update corresponding tests in `wasmtime_runtime_test.go` or `integration_test.go` to reflect your changes.
4.  **Verify Changes**: Run the test suite to ensure everything is working correctly:
    ```bash
    go test ./...
    ```

## Testing

To run the tests, use the standard `go test` command:

```bash
# Fast suite (skips Wasmtime-dependent tests)
CI_FLAG=true go test -v ./...

# Full suite (includes all Wasmtime integration tests)
go test -v ./...
```

Use `CI_FLAG=true` to skip tests that require the Wasmtime runtime or external dependencies.

### Test Files Overview

This project contains three distinct types of tests, each with a different focus:

1.  **`wasmtime_runtime_test.go`**:
    *   **Type**: Unit/Integration Test
    *   **Scope**: Focuses on the interaction with the `WasmtimeRuntime` component (which is implemented in `vela`).
    *   **Purpose**: To verify that this module correctly interacts with the WASM runtime, ensuring robust and correct handling of various scenarios. It tests the module's behavior in isolation when communicating with the runtime, covering happy paths, error conditions (e.g., invalid WASM, corrupted state), and edge cases (e.g., concurrent operations, large payloads).

2.  **`integration_test.go`**:
    *   **Type**: Integration Test
    *   **Scope**: Focuses on the compiled `payment_app.wasm` module.
    *   **Purpose**: To verify that the application logic inside the WASM module is correct. It uses the `WasmtimeRuntime` as a host environment to interact with the WASM binary and test its features (deposit, transfer, withdrawal, etc.), ensuring the business logic is implemented as expected.

3.  **`system_tests/payment_app_system_test.go`**:
    *   **Type**: End-to-End (E2E) System Test
    *   **Scope**: Covers the entire application stack, including simulated components like an "Executor," a "Manager," a database, and a blockchain.
    *   **Purpose**: To validate that all components of the system work together correctly in a production-like environment. It tests the full user flow, including cryptographic operations, request submission, and state verification across the entire distributed system.
    *   **Key tests**:
        -   `TestPaymentAppFullFlow`: Deploys the app, registers user and auditor keys, deposits funds, withdraws funds, and generates a deanonymization report. Validates deposit and withdrawal event fields, verifies on-chain withdrawal recording, checks update payload signatures, and verifies the deanonymization report (framework envelope, base64-encoded report data, and expected user balance after all operations).
    *   **Note**: Skipped when `CI_FLAG=true` due to long execution time.

## Resources

- [TinyGo Documentation](https://tinygo.org/docs/)
- [Wasmtime Documentation](https://docs.wasmtime.dev/)
- [WebAssembly Specification](https://webassembly.org/specs/)
- [WASI Interface](https://wasi.dev/)
