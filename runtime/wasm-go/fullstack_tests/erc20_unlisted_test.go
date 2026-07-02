package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestERC20UnlistedTokenReverts asserts that depositing an ERC-20 that has
// NOT been added to the ProcessorEndpoint's token allowlist reverts on-chain
// with the contract's TokenNotAllowed error. This is the critical
// security gate keeping unvetted ERC-20s out of the protocol — a regression
// here would let anyone deposit arbitrary tokens.
//
// The test skips suite.AddAllowedToken deliberately. The guest-side
// allowlist (DeployApp's allowedTokens) doesn't matter: the contract check
// runs first and short-circuits the request before any WASM execution.
func TestERC20UnlistedTokenReverts(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	sim := suite.GetSimTestHelper()

	// Deploy MockERC20 but INTENTIONALLY do not allowlist it.
	mockAddr := sim.DeployMockERC20("Unlisted Token", "UNL", 18)
	t.Logf("MockERC20 deployed at %s (not allowlisted on ProcessorEndpoint)", mockAddr.Hex())

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	driver := walletTestutil.NewWalletDriver(t, suite)

	// Tell the wallet about UNL so the symbol resolves. Without this the
	// deposit would fail at ResolveToken with "unknown token" — a different
	// error path from what we want to exercise.
	driver.AddToken("UNL", mockAddr, 18)

	// Deploy the payment-app. Listing UNL here only affects the guest-side
	// allowlist; the contract-side gate is independent.
	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", []string{"UNL"})
	require.NoError(t, err)
	require.NotZero(t, appID)

	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))

	// Attempt the deposit. No mint or approve is needed — the contract's
	// allowlist check runs before _pullERC20, so the revert happens without
	// touching the token contract.
	err = driver.Deposit(t.Context(), "100", "UNL", "100 wei")
	require.Error(t, err, "deposit of unlisted token must return an error")

	// The exact wrapping depends on how go-ethereum surfaces the revert
	// reason through the simulated backend. At minimum the error should
	// reference the token-allowlist revert so callers can tell why it failed.
	require.Contains(t, err.Error(), "TokenNotAllowed",
		"expected TokenNotAllowed revert reason in error; got: %v", err)
}
