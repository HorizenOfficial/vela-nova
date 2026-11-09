# Changelog

## 0.3.0

### Features
* **Generic deploy payload** — `novaw deployapp --deploy-payload '<json>'` sends arbitrary JSON as the deploy payload. Mutually exclusive with `--allowed-tokens`.

### Changes
* **Vela v0.3.0** — `vela` and `vela-common-go` bumped from v0.2.0 to v0.3.0 in both modules. Rebuild `payment_app.wasm` and `novaw`.
* **Wasmtime v50** — `wasmtime-go` bumped to v50.0.0 (via `vela`).
* **`load_module` export removed (ABI)** — the WASM module no longer exports `load_module`. Use `deploy`.

