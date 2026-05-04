package main_test

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestPrivateTransferToUnregisteredRecipient proves that a private transfer
// to a recipient who has NOT associated their P521 key on-chain fails
// atomically — the sender's balance is rolled back, no events are emitted,
// and no metadata about the attempted transfer surfaces on-chain or in the
// subgraph. The failure is surfaced via the request's errorCode (not a
// contract revert), so the stateUpdate transaction itself succeeds.
//
// ## Why this matters
//
// The payment-app's `Transfer` path generates two PlainEvents (sender's
// "transfer_sent" and recipient's "transfer_received"). The executor then
// tries to encrypt each event against the recipient's P521 pubkey from its
// keystore. If the recipient was never registered via RegisterUser, the
// keystore lookup fails with CodePubKeyNotRegistered, and the ENTIRE
// request is marked failed — state changes are rolled back and no events
// are emitted.
//
// Regressions that this test catches:
//
//   - **Partial-commit bug:** sender's debit persists despite encryption
//     failure. Would manifest as A's post-failure balance < 1 ETH.
//   - **Metadata leak via failure receipt:** if a future change emitted an
//     unencrypted "transfer_failed" event carrying sender + amount, the
//     subgraph observer learns about the attempt. We assert B never sees
//     anything and A's state is untouched — any surfacing of the transfer
//     details on-chain would break those invariants.
//   - **Unauthenticated auto-registration:** if the executor ever silently
//     created a new keystore entry on-the-fly, the transfer would succeed
//     but with a key the recipient doesn't control (silent fund loss). We
//     assert the transfer fails, not succeeds.
//
// ## Fullstack-unique
//
//   - Mocks stub the executor's keystore; only fullstack exercises the real
//     encryptEvents → CodePubKeyNotRegistered path end-to-end.
//   - Mocks also stub state-rollback on failure; only fullstack proves the
//     real executor rolls back cleanly (sender not debited).
//
// ## Flow
//
//  1. Start suite + executor + manager.
//  2. Two wallet drivers, A and B. Both have funded on-chain accounts and
//     fresh P521 keys inside their own configs — but that key has NOT yet
//     been associated on-chain for B.
//  3. A deploys the payment-app. B.SetApplicationID(appID) — B points its
//     conf at the same app but STILL does not register.
//  4. ONLY A calls RegisterUser. B is deliberately left unregistered.
//  5. A deposits 1 ETH. Assert A's private balance == 1 ETH (pre-condition).
//  6. A attempts a private transfer of 0.3 ETH to B's address. The wallet
//     submits the request on-chain (no client-side pre-flight check exists
//     — the sender has no visibility into B's registration status), the
//     manager forwards it to the executor, the executor fails to encrypt
//     the recipient event, and the request is marked failed.
//  7. Post-failure invariants (all MUST hold):
//       a. PrivateTransfer returned an error ("request has failed" from
//          the wrapper's waitForRequestCompletion path).
//       b. A's private balance is UNCHANGED at 1 ETH — rollback held.
//       c. A's on-chain appCustody is UNCHANGED at 1 ETH — deposit intact.
//       d. B's private balance is nil — unregistered; also, no event
//          indexable to B's address landed in the subgraph.
//       e. AssertNoStateUpdateErrors — the failure is handled via
//          errorCode, not a revert, so the wrapper's error channel stays
//          empty. Any revert would indicate an unexpected on-chain path.
func TestPrivateTransferToUnregisteredRecipient(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	// Step 1.
	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 2: both drivers built identically, both funded; the ONLY
	// asymmetry is step 4 (A registers, B does not).
	driverA := walletTestutil.NewWalletDriver(t, suite)
	driverB := walletTestutil.NewWalletDriver(t, suite)

	// Step 3: A deploys, B is pointed at the same app.
	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driverA.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID)
	driverB.SetApplicationID(appID)

	// Step 4: register A only. This is the critical asymmetry — the whole
	// point of the test. If B were registered, the executor's keystore
	// would resolve B's address to B's P521 pubkey and the transfer would
	// succeed (that's TestPrivateTransfer_TwoUsers).
	require.NoError(t, driverA.RegisterUser(t.Context(), "100 wei"))
	// deliberately NOT: driverB.RegisterUser(...)

	// Step 5: establish the pre-condition — A has funds to attempt to send.
	ethAddr := velacommon.ETH_TOKEN
	oneEther := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	require.NoError(t, driverA.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	balAPre, err := driverA.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balAPre, "A's pre-transfer balance should be visible")
	require.Equal(t, 0, oneEther.Cmp(balAPre),
		"pre-transfer: A should have 1 ETH; got %s wei", balAPre.String())
	require.Equal(t, 0, oneEther.Cmp(suite.GetAppCustody(appID, ethAddr)),
		"pre-transfer: appCustody should reflect the 1 ETH deposit")

	// Step 6: attempt the transfer. B has no P521 pubkey on-chain, so
	// the executor's encryptEvents step will fail with CodePubKeyNotRegistered,
	// the request is marked failed, and the wallet's polling observes the
	// failure and surfaces it as an error.
	err = driverA.PrivateTransfer(t.Context(),
		driverB.UserAddress().Hex(), "0.3 ETH", "", "100 wei")
	require.Error(t, err, "private transfer to unregistered recipient must fail")
	// The executor's encryptEvents returns CodePubKeyNotRegistered (numeric
	// code 9) when the recipient's P521 pubkey is missing from the keystore.
	// That code is embedded in the signed error payload the manager records
	// on-chain and surfaced by the wallet as "request failed (code 9)".
	// Asserting on "code 9" specifically pins the exact failure mode we
	// care about — a different code would indicate a different rejection
	// path (e.g., insufficient balance, invalid payload), which would mean
	// this test isn't testing what it thinks it's testing.
	require.Contains(t, err.Error(), "code 9",
		"expected PubKeyNotRegistered rejection (error code 9 from apperrors.CodePubKeyNotRegistered); got: %v",
		err,
	)

	// Step 7a: A's private balance must be UNCHANGED — rollback held.
	// A single wei of deviation here = a partial-commit bug (sender debited
	// despite the transfer failing).
	balAPost, err := driverA.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balAPost, "A's post-failure balance must still be visible")
	require.Equal(t, 0, oneEther.Cmp(balAPost),
		"post-failure: A's balance must be UNCHANGED at 1 ETH (rollback invariant); got %s wei", balAPost.String())

	// Step 7b: on-chain custody untouched. Private transfers don't move
	// on-chain funds anyway, so this would only change if some unrelated
	// bug caused the request to take a different path.
	require.Equal(t, 0, oneEther.Cmp(suite.GetAppCustody(appID, ethAddr)),
		"post-failure: appCustody must be UNCHANGED at 1 ETH")

	// Step 7c: B sees nothing. Unregistered anyway (so the wallet can't
	// decrypt events addressed to B), but additionally the executor
	// never emitted any events for this failed request.
	balB, err := driverB.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.Nil(t, balB,
		"B must have no private balance — unregistered AND the failed transfer should not have produced any event")

	// Step 7d: the stateUpdate for the failed request carries errorCode != 0
	// but is NOT a revert; the tx succeeds on-chain. The wrapper's error
	// channel should therefore be empty. A non-empty channel would flag
	// that the manager hit an unexpected on-chain error during this
	// supposedly-clean failure path.
	suite.AssertNoStateUpdateErrors(t)

	t.Logf("Private transfer to unregistered recipient failed cleanly: %v", err)
}
