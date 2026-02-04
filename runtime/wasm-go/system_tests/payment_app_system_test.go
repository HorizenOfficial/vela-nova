package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/logger"
	systemTests "github.com/horizen-pes/pkg/testutil"
	"github.com/stretchr/testify/require"
)

// buildAndLoadWasmModule is a helper function to build the wasm module and read its bytecode.
func buildAndLoadWasmModule(t *testing.T) []byte {
	// Get the project root directory to construct absolute paths
	_, b, _, ok := runtime.Caller(0)
	require.True(t, ok)
	//appDir := filepath.Join(filepath.Dir(b), "../..")
	projectRoot := filepath.Join(filepath.Dir(b), "../..")
	appDir := filepath.Join(projectRoot, "wasm-go")

	// Build the wasm module
	cmd := exec.Command("make", "build")
	cmd.Dir = appDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to build wasm module: %s", string(output))

	// Load wasm bytecode for the wasm app
	wasmPath := filepath.Join(appDir, "build", "payment_app.wasm")
	wasmBytecode, err := os.ReadFile(wasmPath)
	require.NoError(t, err)
	require.NotEmpty(t, wasmBytecode)

	return wasmBytecode
}

func TestWasmtimePaymentAppFullSystemFlow(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping long running test in CI environment")
	}

	suite := systemTests.NewSystemTestSuite(t, "wasmtime-payment", newTestLogger(), newTestLogger())
	defer suite.Cleanup()

	// Build and load wasm bytecode
	wasmBytecode := buildAndLoadWasmModule(t)

	systemTests.ExecTestAppFullSystemFlow(t, suite, wasmBytecode)
}

func newTestLogger() logger.Logger {
	testLogger := logger.NewLogger(
		&logger.Config{
			Kind:         "zeronetwork",
			ConsoleColor: false, // colors can print escape chars on tty
			Console:      false,
			ConsoleLevel: "trace",
			//FileName:     "qqq.log",
			FileLevel:        "trace",
			RemoteLogParams:  common.TcpChannelConnectionParams{Ip: "localhost", Port: 5000},
			RemoteLogNetwork: "tcp",
			NetworkLevel:     "trace"},
	)
	return testLogger
}
