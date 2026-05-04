package main_test

import (
	"os"
	"path/filepath"
	"testing"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestGetPendingPaymentsMatchesContract proves the wallet's GetPendingPayments
// command returns the same numeric value the ProcessorEndpoint contract holds
// in pendingClaims[ETH][user], and that the wallet's ClaimPendingPayments
// command zeros it out. Distinct from TestFeeRefundClaim, which exercises the
// accumulator and uses sim.Claim (the public claim path) bypassing the wallet:
// this test pins the read+claim flow specifically through the wallet code path.
//
// ## What's being exercised
//
//   - GetPendingPaymentsCommand.Exec → BlockchainClient.GetPendingClaims:
//     numeric agreement with the contract
//   - ClaimPendingPaymentsCommand.Exec → BlockchainClient.Claim: clears the
//     accumulator end-to-end through the wallet (TestFeeRefundClaim uses
//     sim.Claim instead, so this is the only fullstack assertion that the
//     wallet's claim wrapper actually clears state)
//
// ## Flow
//
//  1. Suite + executor + manager + driver.
//  2. Three happy-path requests (deploy + register + deposit) at maxFee=100
//     wei, accumulating non-trivial fee refunds in pendingClaims[ETH][user].
//  3. walletAmount = driver.GetPendingPayments — read via the wallet path.
//     contractAmount = sim.GetPendingClaims — read via the chain directly.
//     Assert walletAmount == contractAmount > 0.
//  4. driver.ClaimPendingPayments — drive the wallet's claim path.
//  5. Re-read both: walletAmount == contractAmount == 0.
//  6. AssertNoStateUpdateErrors — pure happy-path; the wrapper's error
//     channel should be empty.
func TestGetPendingPaymentsMatchesContract(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	driver := walletTestutil.NewWalletDriver(t, suite)
	user := driver.UserAddress()

	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID)
	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))
	require.NoError(t, driver.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	// Step 3: wallet's view should equal the contract's view, and be > 0.
	sim := suite.GetSimTestHelper()
	ethAddr := velacommon.ETH_TOKEN

	walletAmount, err := driver.GetPendingPayments(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, 1, walletAmount.Sign(),
		"wallet should report a strictly positive pending claim after three successful requests; got %s",
		walletAmount.String())

	contractAmount := sim.GetPendingClaims(ethAddr, user)
	require.Equal(t, 0, walletAmount.Cmp(contractAmount),
		"wallet's view of pending claims (%s) must equal the contract's view (%s)",
		walletAmount.String(), contractAmount.String())

	// Step 4: drive the claim through the wallet path. Distinct from
	// TestFeeRefundClaim, which uses sim.Claim and bypasses the wallet.
	require.NoError(t, driver.ClaimPendingPayments(t.Context(), ""))

	// Step 5: both views should now read zero. If the wallet's claim wrapper
	// silently no-op'd, walletAmount would still match contractAmount but
	// both would be non-zero — caught by the > 0 check inverted.
	walletAfter, err := driver.GetPendingPayments(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, 0, walletAfter.Sign(),
		"wallet should report zero pending claims after ClaimPendingPayments; got %s",
		walletAfter.String())

	contractAfter := sim.GetPendingClaims(ethAddr, user)
	require.Equal(t, 0, contractAfter.Sign(),
		"contract should report zero pending claims after ClaimPendingPayments; got %s",
		contractAfter.String())

	// Step 6: pure happy-path; nothing should have reverted on the manager's
	// stateUpdate path.
	suite.AssertNoStateUpdateErrors(t)
}
