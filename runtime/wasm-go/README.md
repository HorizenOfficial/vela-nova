# WASM Go Runtime

This module contains the Go implementation of a WASM runtime for the Horizen PES project.

## Overview

The WebAssembly runtime system consists of:
- **TinyGo**: Compiles Go code to WebAssembly modules.
- **Wasmtime-Go**: Executes WebAssembly modules in a secure, sandboxed environment.
- **Payment App**: An example WASM application for handling deposits, transfers, and withdrawals.

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

TODO: This will change when the github public repo will be available.

This module depends on the `horizen-pes` repository, specifically the `dev` branch.
Make sure you have the `horizen-pes` repository checked out at the `dev` branch and that it is in your Go path.

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

## Development Workflow

1.  **Modify WASM Module**: The core application logic is in `main.go` and `app/app.go`. Utility functions are located in `utils/`.
2.  **Rebuild Module**: After making changes, rebuild the WASM module using `make build` or the `tinygo` command directly.
3.  **Update Tests**: Add or update corresponding tests in `wasmtime_runtime_test.go`, `integration_test.go`, or `system_tests/payment_app_system_test.go` to reflect your changes.
4.  **Verify Changes**: Run the test suite to ensure everything is working correctly:
    ```bash
    go test ./...
    ```

## Testing

To run the tests, use the standard `go test` command:

```bash
go test ./...
```


### Test Files Overview

This project contains three distinct types of tests, each with a different focus:

1.  **`wasmtime_runtime_test.go`**:
    *   **Type**: Unit/Integration Test
    *   **Scope**: Focuses on the `WasmtimeRuntime` component itself.
    *   **Purpose**: To verify that the Go implementation of the WASM runtime is robust, correct, and handles various scenarios as expected. It directly calls the runtime's functions to test its behavior in isolation, covering happy paths, error conditions (e.g., invalid WASM, corrupted state), and edge cases (e.g., concurrent operations, large payloads).

2.  **`integration_test.go`**:
    *   **Type**: Integration Test
    *   **Scope**: Focuses on the compiled `payment_app.wasm` module.
    *   **Purpose**: To verify that the application logic inside the WASM module is correct. It uses the `WasmtimeRuntime` as a host environment to interact with the WASM binary and test its features (deposit, transfer, withdrawal, etc.), ensuring the business logic is implemented as expected.

3.  **`system_tests/payment_app_system_test.go`**:
    *   **Type**: End-to-End (E2E) System Test
    *   **Scope**: Covers the entire application stack, including simulated components like an "Executor," a "Manager," a database, and a blockchain.
    *   **Purpose**: To validate that all components of the system work together correctly in a production-like environment. It tests the full user flow, including cryptographic operations, request submission, and state verification across the entire distributed system.

## Resources

- [TinyGo Documentation](https://tinygo.org/docs/)
- [Wasmtime Documentation](https://docs.wasmtime.dev/)
- [WebAssembly Specification](https://webassembly.org/specs/)
- [WASI Interface](https://wasi.dev/)
