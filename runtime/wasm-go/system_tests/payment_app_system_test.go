package main_test

import (
	"os"
	"testing"

	systemTests "github.com/horizen-pes/pkg/testutil"
)

func TestWasmtimePaymentAppFullSystemFlow(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment")
	defer suite.Cleanup()
	// Load wasm bytecode for the payment app
	wasmBytecode := suite.LoadWasmModule(t, "../build/payment_app.wasm")
	systemTests.ExecTestAppFullSystemFlow(t, suite, wasmBytecode)
}
