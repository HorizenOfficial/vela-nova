package testutil_test

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/executor"
	"github.com/HorizenOfficial/vela/pkg/logger"
	"github.com/HorizenOfficial/vela/pkg/manager"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestWalletDriver_DeployRegisterDeposit is the end-to-end smoke for the
// WalletDriver harness: it drives three wallet commands (DeployApp,
// RegisterUser, Deposit) against a real simulated chain + in-process
// subgraph + in-process authority + mock-runtime executor. Assertions target
// on-chain custody and subgraph state rather than stdout — the driver
// returns structured errors, and the subgraph is the same source the
// wallet's WaitForRequestCompleted polls.
//
// The runtime is mock-runtime so no TinyGo build is required; any bytes are
// accepted as the "WASM" artifact. This keeps the test hermetic.
func TestWalletDriver_DeployRegisterDeposit(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("skipping fullstack test under CI_FLAG")
	}

	suite := newSuite(t)
	defer func() { _ = suite.Cleanup() }()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	driver := testutil.NewWalletDriver(t, suite)

	// Placeholder WASM — mock-runtime ignores contents.
	wasmPath := filepath.Join(t.TempDir(), "app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, []byte("mock-wasm-bytes"), 0o644))

	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID, "DeployApp must assign a non-zero ApplicationID")
	require.Equal(t, appID, driver.ApplicationID(), "driver should reflect persisted ApplicationID")

	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))

	// Deposit 0.1 ETH (1e17 wei).
	require.NoError(t, driver.Deposit(t.Context(), "0.1 ETH", "", "100 wei"))

	// On-chain custody for ETH (address 0) must reflect the deposit.
	ethAddr := suite.GetSimTestHelper().Submitter.From
	_ = ethAddr // avoid unused; keep in case debugging needs sender context
	ethZero := [20]byte{}
	custody := suite.GetAppCustody(appID, ethZero)
	expected := new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil) // 1e17
	require.Equal(t, 0, custody.Cmp(expected),
		"on-chain appCustody for ETH should equal 0.1 ETH; got %s wei", custody.String())
}

func newSuite(t *testing.T) *fullstack.FullStackSystemTestSuite {
	t.Helper()
	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())
	t.Setenv("EXECUTOR_KEYSET_RECOVERY_TYPE", "0")
	mgrCfg, err := manager.LoadConfig()
	require.NoError(t, err)
	execCfg, err := executor.LoadConfig()
	require.NoError(t, err)
	keySet, recovery, err := executor.GenerateEnclaveKeySet(t.Context(), execCfg.KeySetRecoveryType, nil, nil, "")
	require.NoError(t, err)
	logCfg := &logger.Config{Kind: "zerolog", Console: true, ConsoleLevel: "info"}
	return fullstack.NewFullStackSystemTestSuiteWithConfigs(t, "mock-runtime", mgrCfg, execCfg, keySet, recovery, logCfg, logCfg)
}
