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

// TestDeployerRoleEnforcement asserts both directions of the ProcessorEndpoint
// DEPLOYER_ROLE access-control invariant:
//
//  1. A user WITHOUT DEPLOYER_ROLE cannot deploy — the contract reverts with
//     DeployerNotAllowed (see ProcessorEndpoint.sol's deployRequest guard).
//  2. After the admin grants DEPLOYER_ROLE to that same user, the EXACT SAME
//     deploy call succeeds.
//
// This is the one invariant the existing fullstack suite does not explicitly
// verify. Every current wallet-driven deploy test uses NewWalletDriver, which
// grants DEPLOYER_ROLE unconditionally during construction — so the tests
// exercise the role-granted path but never the rejection path. If the
// contract-side check was accidentally removed or short-circuited to true,
// all 7 current fullstack tests would still pass. This test catches that
// regression.
//
// The test is fullstack-unique: mocks stub the contract, so they cannot
// assert that the role gate is actually present and enforced.
//
// Flow:
//  1. Start suite, executor, manager.
//  2. Build driver via NewWalletDriverNoDeployerRole — creates a funded user
//     WITHOUT the role grant (single-purpose constructor; NewWalletDriver's
//     role-granting behavior is unchanged).
//  3. Attempt DeployApp → expect error containing "DeployerNotAllowed".
//  4. suite.GrantDeployerRole(driver.UserAddress()) — admin grants the role.
//  5. Retry DeployApp → expect success, non-zero appID.
func TestDeployerRoleEnforcement(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Build a driver that is funded + registered, but has NOT been granted
	// DEPLOYER_ROLE. Parallel constructor — existing NewWalletDriver (used by
	// every other test) still grants the role; this variant exists solely for
	// access-control tests like this one.
	driver := walletTestutil.NewWalletDriverNoDeployerRole(t, suite)

	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	// Step 3: deploy without the role — must be rejected on-chain.
	// The simulated backend surfaces the revert reason through the tx
	// submission error, wrapped by the wallet's DeployApp wrapper.
	_, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.Error(t, err, "DeployApp must fail when user lacks DEPLOYER_ROLE")
	require.Contains(t, err.Error(), "DeployerNotAllowed",
		"expected DeployerNotAllowed revert reason in error; got: %v", err)

	// Step 4: admin grants DEPLOYER_ROLE to this user.
	// GrantDeployerRole is a simHelper call — runs as the constructor-deployer
	// (admin) which holds ADMIN role, the only role that can grant DEPLOYER_ROLE.
	suite.GrantDeployerRole(driver.UserAddress())

	// Step 5: retry — exact same call, same user, same payload. Only the
	// role state changed. Success here proves the grant is what unblocks
	// deploy (not some other incidental state).
	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err, "DeployApp must succeed after DEPLOYER_ROLE is granted")
	require.NotZero(t, appID, "successful deploy must assign a non-zero ApplicationID")

	t.Logf("Deploy rejected without DEPLOYER_ROLE; same user succeeded (app %d) after grant", appID)
}
