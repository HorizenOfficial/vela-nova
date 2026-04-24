package main_test

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// TestMultiAppIsolation is the multi-tenancy isolation test. It deploys two
// independent payment applications on the same simulated chain, registers
// the SAME user in both, and then verifies that every piece of state is
// scoped per-appID and cannot bleed across the boundary. A regression that
// shared state between apps would be catastrophic (cross-tenant data leak),
// so this invariant is worth a dedicated end-to-end test.
//
// ## What this uniquely catches
//
// Four layers of isolation are exercised in one flow:
//
//  1. **On-chain `appCustody[appID][token]`** — the ProcessorEndpoint
//     contract keys deposited funds by (appID, token). A collapsed/shared
//     map would let A's deposit show up in B's custody balance.
//
//  2. **Executor's encrypted state storage** — the versioned LevelDB layer
//     stores each app's encrypted state under an appID-derived key. A bug
//     that dropped/collapsed the appID portion would surface as the
//     executor decrypting A's state when processing a request for B (or vice
//     versa), corrupting both.
//
//  3. **Subgraph event filter** — `GetPrivateBalance` queries the subgraph
//     with an `applicationID` filter. A missing/wrong filter would leak A's
//     encrypted events into B's balance scan (the wallet would then try to
//     decrypt them, typically succeed because the same user key is used in
//     both apps, and report a wrong balance).
//
//  4. **WASM runtime instance separation** — the executor runs each app as
//     its own Wasmtime module instance; runtime-level leakage (e.g. a shared
//     module cache that accidentally shared memory) would also show up here.
//
// ## Design choices
//
//   - **One user across both apps.** This is the tighter invariant: if
//     events or storage were mis-keyed by user address (but happened to
//     stay correctly keyed by appID), a two-distinct-users test would
//     silently pass. With a single shared address, isolation MUST hold
//     strictly by appID — there is no secondary key that could save us.
//
//   - **ETH only.** ERC-20 plumbing (approve, mint, decimals) is orthogonal
//     and already covered by `TestERC20FullStack` and the ERC-20 private
//     transfer test. Keeping this ETH-only means any assertion failure
//     points directly at an isolation bug, not a token-path issue.
//
//   - **Same WASM bytes for both apps.** Apps differ on-chain by appID
//     alone, so reusing the artifact makes the test strictly about the
//     harness/executor/subgraph plumbing rather than per-app application
//     logic.
//
// ## Flow (8 steps)
//
//  1. Start suite, executor, manager.
//  2. Build one wallet driver (single user, single key).
//  3. Deploy app A; same driver registers the user in A.
//  4. Deploy app B; same driver registers the user in B. DeployApp
//     overwrites the driver's current ApplicationID, so after this the
//     driver "points at" B.
//  5. Sanity pre-check — both custodies zero, both private balances nil.
//  6. Deposit 1 ETH into A. Assert A's custody = 1 ETH AND B's custody
//     still 0. Assert A's private balance = 1 ETH AND B's private balance
//     still nil. Any of these failing = a real isolation bug.
//  7. Deposit 0.5 ETH into B. Assert B's custody = 0.5 ETH AND A's custody
//     still 1 ETH (unchanged by the B-side deposit). Assert B's private
//     balance = 0.5 ETH AND A's private balance still 1 ETH.
//  8. Withdraw 0.3 ETH from A only. Assert A's custody = 0.7 ETH AND B's
//     custody still 0.5 ETH (state-mutation on one app must not touch the
//     other). Assert A's private balance = 0.7 ETH AND B's private balance
//     still 0.5 ETH.
func TestMultiAppIsolation(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	// Step 1: executor before manager — manager handshakes with executor at startup.
	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 2: a single wallet driver. One user, one secp256k1 key, one P521
	// key. The SAME key-pair is used against both apps below — that is the
	// whole point of this test.
	driver := walletTestutil.NewWalletDriver(t, suite)

	// Shared WASM bytes: both apps run the same payment-app. Any isolation
	// we observe in the assertions below is attributable strictly to the
	// harness plumbing, not to application-level code differences.
	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	// Step 3: deploy A and register user in A.
	// DeployApp assigns a fresh ApplicationID server-side and persists it
	// into the driver's temp wallet.conf. The follow-up RegisterUser call
	// therefore targets app A automatically.
	appAID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appAID)
	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))

	// Step 4: deploy B and register the same user in B.
	// DeployApp overwrites the driver's ApplicationID with appBID, so the
	// subsequent RegisterUser scopes to B. After this block the driver
	// "points at" B — we hop back to A explicitly via SetApplicationID
	// whenever we need to.
	appBID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appBID)
	require.NotEqual(t, appAID, appBID, "the two apps must receive distinct IDs (contract allocates sequentially)")
	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))

	// Handy constants reused across the assertions.
	ethAddr := ethCommon.Address{} // sentinel for native ETH in appCustody
	zero := big.NewInt(0)
	oneEther := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	halfEther := new(big.Int).Div(oneEther, big.NewInt(2))
	threeTenthsEther := new(big.Int).Mul(big.NewInt(3), new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil)) // 0.3 ETH
	sevenTenthsEther := new(big.Int).Sub(oneEther, threeTenthsEther)                                          // 0.7 ETH

	// Step 5: sanity pre-check — both apps MUST start empty. If either
	// starts non-zero, something funky happened during Deploy/Register (e.g.
	// crossed state from the other app) and the rest of the test would be
	// meaningless.
	require.Equal(t, 0, zero.Cmp(suite.GetAppCustody(appAID, ethAddr)),
		"A's custody should start at 0")
	require.Equal(t, 0, zero.Cmp(suite.GetAppCustody(appBID, ethAddr)),
		"B's custody should start at 0")

	// Private balance is queried by flipping the driver's ApplicationID; the
	// wrapper then scans the subgraph scoped to the current app.
	driver.SetApplicationID(appAID)
	balAInit, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.Nil(t, balAInit, "A's private balance should be nil before any deposit")

	driver.SetApplicationID(appBID)
	balBInit, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.Nil(t, balBInit, "B's private balance should be nil before any deposit")

	// Step 6: deposit 1 ETH into A (hop driver back to A first).
	// Two critical cross-app checks after this:
	//   - on-chain isolation: B's appCustody must remain 0 — if appCustody
	//     were collapsed to a single map, A's deposit would show in B.
	//   - subgraph/decrypt isolation: querying B's private balance must still
	//     return nil — if the subgraph's applicationID filter or the wallet's
	//     decrypt pipeline ignored appID, B would "see" A's deposit event.
	driver.SetApplicationID(appAID)
	require.NoError(t, driver.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	require.Equal(t, 0, oneEther.Cmp(suite.GetAppCustody(appAID, ethAddr)),
		"A's custody should be 1 ETH after A's deposit")
	require.Equal(t, 0, zero.Cmp(suite.GetAppCustody(appBID, ethAddr)),
		"B's custody must remain 0 — A's deposit leaked into B's appCustody")

	balA1, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balA1, "A's private balance should be visible after A's deposit")
	require.Equal(t, 0, oneEther.Cmp(balA1), "A's private balance should be 1 ETH")

	driver.SetApplicationID(appBID)
	balB1, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.Nil(t, balB1, "B's private balance must remain nil — A's deposit leaked into B's events")

	// Step 7: deposit 0.5 ETH into B (driver already points at B).
	// Mirror checks: A's state must be UNCHANGED by B's deposit. This
	// catches any bug where B's deposit transaction (appCustody write, event
	// emission, executor state update) wasn't strictly scoped to appBID.
	require.NoError(t, driver.Deposit(t.Context(), "0.5 ETH", "", "100 wei"))

	require.Equal(t, 0, halfEther.Cmp(suite.GetAppCustody(appBID, ethAddr)),
		"B's custody should be 0.5 ETH after B's deposit")
	require.Equal(t, 0, oneEther.Cmp(suite.GetAppCustody(appAID, ethAddr)),
		"A's custody must remain 1 ETH — B's deposit perturbed A's appCustody")

	balB2, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balB2, "B's private balance should be visible after B's deposit")
	require.Equal(t, 0, halfEther.Cmp(balB2), "B's private balance should be 0.5 ETH")

	driver.SetApplicationID(appAID)
	balA2, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balA2, "A's private balance should still be visible")
	require.Equal(t, 0, oneEther.Cmp(balA2),
		"A's private balance must remain 1 ETH — B's deposit leaked into A's view")

	// Step 8: withdraw 0.3 ETH from A only.
	// Deposits prove that writes don't leak. Withdrawals additionally prove
	// that READS-then-WRITES scope correctly: a withdraw reads A's balance,
	// debits it, and writes back. A bug that scoped the read or write to
	// "latest app" rather than appID would drain B's custody here.
	userHex := driver.UserAddress().Hex()
	require.NoError(t, driver.Withdraw(t.Context(), "0.3 ETH", userHex, "", "100 wei"))

	require.Equal(t, 0, sevenTenthsEther.Cmp(suite.GetAppCustody(appAID, ethAddr)),
		"A's custody should be 0.7 ETH after 0.3 ETH withdraw")
	require.Equal(t, 0, halfEther.Cmp(suite.GetAppCustody(appBID, ethAddr)),
		"B's custody must remain 0.5 ETH — A's withdraw touched B's custody")

	balA3, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balA3)
	require.Equal(t, 0, sevenTenthsEther.Cmp(balA3),
		"A's private balance should be 0.7 ETH after the withdraw")

	driver.SetApplicationID(appBID)
	balB3, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balB3)
	require.Equal(t, 0, halfEther.Cmp(balB3),
		"B's private balance must remain 0.5 ETH — A's withdraw perturbed B's view")

	t.Log("Multi-app isolation verified: on-chain custody, subgraph event filter, executor state, and private-balance decrypt all scope correctly by appID")
}
