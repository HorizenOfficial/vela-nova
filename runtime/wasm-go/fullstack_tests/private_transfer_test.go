package main_test

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestPrivateTransfer_TwoUsers exercises the payment-app's central feature —
// encrypted transfers between two registered users — end-to-end through the
// wallet CLI layer. Fullstack-unique value: validates that subgraph event
// indexing delivers *distinct* encrypted events to sender vs recipient, and
// that each party can only decrypt its own side (sender sees a
// "transfer_sent" event with their new balance; recipient sees a
// "transfer_received" event with their balance).
//
// Flow:
//  1. Two drivers = two funded users. Each driver has its own temp wallet.conf,
//     P521 key, and blockchain client bound to its own secp key.
//  2. User A deploys the payment app. User B points its conf at the same
//     ApplicationID (SetApplicationID).
//  3. Both register. A deposits 1 ETH. A private-transfers 0.3 ETH to B.
//  4. Assert A's private balance == 0.7 ETH (via A's driver, which only
//     decrypts events addressed to A).
//  5. Assert B's private balance == 0.3 ETH (via B's driver, decrypting its
//     own events). If event-subtype derivation is broken, one or both reads
//     would return nil instead of the expected amount.
func TestPrivateTransfer_TwoUsers(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Two independent wallet drivers = two funded users, two P521 keys,
	// two wallet.conf files. Each driver grants its user DEPLOYER_ROLE
	// (harmless redundancy; only one will deploy).
	driverA := walletTestutil.NewWalletDriver(t, suite)
	driverB := walletTestutil.NewWalletDriver(t, suite)

	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	// A deploys; B gets told about the resulting ApplicationID so its
	// commands (RegisterUser, GetPrivateBalance) resolve to the same app.
	appID, err := driverA.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID)
	driverB.SetApplicationID(appID)

	// Both users associate their P521 keys with the app. Seed registration
	// is what makes subtype-filtered event queries work — without it, each
	// user would fail to fetch their own events.
	require.NoError(t, driverA.RegisterUser(t.Context(), "100 wei"))
	require.NoError(t, driverB.RegisterUser(t.Context(), "100 wei"))

	// A deposits 1 ETH. Verify A's private balance reflects the deposit.
	require.NoError(t, driverA.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	oneEther := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	balA, err := driverA.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balA, "A should see a balance event after deposit")
	require.Equal(t, 0, oneEther.Cmp(balA),
		"A's post-deposit balance should be 1 ETH; got %s wei", balA.String())

	// B should see no balance yet — nothing has arrived.
	balB0, err := driverB.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.Nil(t, balB0, "B should have no private balance before the transfer")

	// A private-transfers 0.3 ETH to B. This generates two encrypted events
	// on the same state update: one routed to A ("transfer_sent") with A's
	// post-transfer balance, one to B ("transfer_received") with B's new
	// balance. The subgraph indexes both.
	require.NoError(t, driverA.PrivateTransfer(t.Context(),
		driverB.UserAddress().Hex(), "0.3 ETH", "", "100 wei"))

	transferred := new(big.Int).Mul(big.NewInt(3), new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil)) // 0.3 ETH
	remaining := new(big.Int).Sub(oneEther, transferred)                                                  // 0.7 ETH

	balA2, err := driverA.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balA2, "A should see a balance event after sending the transfer")
	require.Equal(t, 0, remaining.Cmp(balA2),
		"A's post-transfer balance should be 0.7 ETH; got %s wei", balA2.String())

	balB, err := driverB.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balB, "B should see a balance event after receiving the transfer")
	require.Equal(t, 0, transferred.Cmp(balB),
		"B's post-transfer balance should be 0.3 ETH; got %s wei", balB.String())

	t.Log("Private transfer between two users — balances, subgraph indexing, and per-user decryption all verified")
}
