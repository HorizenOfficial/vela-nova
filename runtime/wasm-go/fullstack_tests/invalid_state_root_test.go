package main_test

import (
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestInvalidPrevStateRootRejection proves that ProcessorEndpoint.stateUpdate
// rejects an update whose prevStateRoot does not match the contract's
// stored root for the target app. This guard is what prevents:
//
//   - Reorg-induced state drift: if the chain reorged past the manager's
//     latest known state, resubmitting a stale update would silently
//     overwrite newer on-chain state.
//   - Replay of a stale stateUpdate by any UPDATE_STATUS_ROLE-holder.
//   - Divergent concurrent managers submitting incompatible updates.
//
// Fullstack-unique:
//   - Hardhat tests can exercise the prevStateRoot comparison in isolation
//     against a unit-test contract instance.
//   - Only fullstack proves the wiring: that the check sits on the real
//     stateUpdate path, fires in the right order (after role, requestID
//     and appID checks, BEFORE the TEE signature check), and surfaces the
//     InvalidStateRoot selector through UnpackProcessorEndpointError.
//
// ## Attack path
//
// The contract's stateUpdate performs guards in this order (see
// ProcessorEndpoint.sol):
//
//  1. onlyRole(UPDATE_STATUS_ROLE)                   — S3 tests this.
//  2. isCurrentPendingRequest(processedRequestId)    — InvalidRequestId.
//  3. applicationId == requestInfo.applicationId     — InvalidApplicationId.
//  4. prevStateRoot == applicationStateRoots[appId]  — InvalidStateRoot (THIS test).
//  5. teeAuthenticator.checkSignature(...)           — S2 tests this.
//
// To trigger InvalidStateRoot specifically we need to pass guards 1-3 and
// fail guard 4:
//
//   - Sign with the manager's key (guard 1). Done via SubmitStateUpdateAsManager.
//   - Use a real pending request ID (guard 2). A freshly-submitted request
//     that the manager has not yet processed is the natural fit.
//   - Use the matching application ID (guard 3).
//   - Use a stale prev-state-root: zero bytes is the unambiguous "wrong"
//     value once the app has been deployed, since deploy + register +
//     deposit have advanced the stored root to a non-zero value.
//
// ## Flow
//
//  1. Start suite + executor + manager. Build driver. Deploy the app,
//     register the user, deposit 1 ETH — three happy-path state
//     transitions. applicationStateRoots[appID] is now unambiguously a
//     non-zero value V.
//  2. suite.StopManager() — we are about to submit a stateUpdate as the
//     manager ourselves; stopping the manager prevents nonce contention
//     between its polling loop and our direct submission.
//  3. Submit a fresh deposit request DIRECTLY via SimTestHelper (no
//     driver wrapper — we don't want to block waiting for completion,
//     since the stopped manager will never complete it). Extract the
//     requestID from the RequestSubmitted event. This satisfies guard 2.
//  4. Craft a stateUpdate payload pairing that real requestID with a
//     DELIBERATELY stale prev-state-root ([32]byte{}). Since the app's
//     real stored root is non-zero, guard 4 must reject.
//  5. suite.SubmitStateUpdateAsManager(payload) → expect the Go unpacker
//     to surface "InvalidStateRoot".
//  6. Sanity cross-check: AssertNoStateUpdateErrors — the
//     SubmitStateUpdateAsManager path builds a fresh client and bypasses
//     the wrapped eventBroadcastingClient, so its error channel must be
//     empty. A non-empty channel would indicate the bypass is broken or
//     the (stopped) manager somehow leaked an error into the wrapper.
func TestInvalidPrevStateRootRejection(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 1: three happy-path stateUpdates move applicationStateRoots[appID]
	// from zero to some non-zero value V. Exact value doesn't matter — we
	// only need "non-zero" so that [32]byte{} is unambiguously wrong.
	driver := walletTestutil.NewWalletDriver(t, suite)

	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID)
	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))
	require.NoError(t, driver.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	// Step 2: stop the running manager. After this, no stateUpdates happen
	// via the manager's polling loop — the test owns the manager-signed
	// submission path.
	require.NoError(t, suite.StopManager())

	// Step 3: submit a fresh deposit request directly on-chain. Using
	// SimTestHelper.SubmitRequestFromUser bypasses the wallet driver's
	// wait-for-completion logic; we need the request to sit in the
	// pending queue unprocessed so it satisfies guard 2 when our bogus
	// stateUpdate references it.
	sim := suite.GetSimTestHelper()
	user := driver.UserAddress()
	userOpts, err := suite.GetTransactOpts(user)
	require.NoError(t, err)

	oneTenthEth := new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil) // 0.1 ETH
	maxFee := big.NewInt(100)                                           // 100 wei
	depositTx := sim.SubmitRequestFromUser(
		appID,
		common.Process,
		nil, // empty payload — guard 2 doesn't inspect it
		velacommon.ETH_TOKEN,
		oneTenthEth,
		maxFee,
		userOpts,
	)
	sim.WaitMined(depositTx)
	requestID := sim.GetRequestSubmittedEvent(depositTx).RequestId

	// Step 4: craft the bogus payload. Zero prev-root is the unambiguous
	// "obviously wrong" choice once the app has been deployed. ErrorCode,
	// empty signature, etc. are irrelevant — guard 4 fires before guard 5
	// reads them.
	bogus := &common.UpdatePayload{
		ApplicationID:  appID,
		RequestID:      requestID,
		PrevStateRoot:  [32]byte{},     // deliberately stale: real stored root is non-zero
		NewStateRoot:   [32]byte{0xAA}, // arbitrary
		Events:         []common.Event{},
		AppEvents:      []common.AppEvent{},
		Withdrawals:    []common.Withdrawal{},
		Signature:      []byte{}, // guard 5 would reject, but guard 4 fires first
		RefundAmount:   common.NewBig(0),
		ApplicationFee: common.NewBig(0),
		ErrorCode:      0,
		ErrorMsg:       "",
	}

	// Step 5: submit as the manager (guard 1 passes). Guards 2 and 3 pass
	// because we used a real pending requestID for the matching appID.
	// Guard 4 fails because prev=zero ≠ stored non-zero root.
	err = suite.SubmitStateUpdateAsManager(bogus)
	require.Error(t, err, "stateUpdate with stale prevStateRoot must be rejected")
	require.True(t,
		strings.Contains(err.Error(), "InvalidStateRoot"),
		"expected InvalidStateRoot revert (ProcessorEndpoint guard 4); got: %v", err,
	)

	// Step 6: the bypass path (fresh client via SubmitStateUpdateAsManager)
	// must not leak errors into the wrapped eventBroadcastingClient's
	// buffer. A non-empty buffer would indicate either (a) the bypass was
	// inadvertently re-routed through the wrapper, or (b) the stopped
	// manager somehow emitted an erroring update before we took over —
	// both would invalidate the isolation this test relies on.
	suite.AssertNoStateUpdateErrors(t)

	// Belt-and-braces: the failed stateUpdate must have reverted the whole
	// tx, so applicationStateRoots[appID] must still equal the happy-path
	// value V. We don't know V's exact value but we know it's NOT zero
	// and NOT our bogus 0xAA root.
	stored := sim.GetStateRoot(appID)
	require.NotEqual(t, [32]byte{}, stored,
		"after failed stateUpdate, stored root must remain non-zero (happy-path value V)")
	require.NotEqual(t, [32]byte{0xAA}, stored,
		"after failed stateUpdate, stored root must NOT be our bogus newStateRoot (rollback held)")

	t.Logf("stateUpdate with stale prevStateRoot correctly rejected: %v", err)
}
