package main_test

import (
	"os"
	"strings"
	"testing"

	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestNonManagerStateUpdateReverts proves that ProcessorEndpoint.stateUpdate
// is gated by UPDATE_STATUS_ROLE and rejects any caller other than the
// manager account (the sole role-holder granted at contract construction).
// If that gate were ever removed or relaxed, any funded account could
// forge state updates on-chain — catastrophic.
//
// Sibling of TestTEEAttestationRejection (S2):
//   - S2 proves the *signature* embedded in the state update is verified.
//   - This test proves the *caller* of stateUpdate is authorised.
//
// Both guards must hold for the security model to work; each is independent
// of the other. A regression on either would pass all other fullstack tests
// silently.
//
// Fullstack-unique:
//   - OpenZeppelin's AccessControl is tested in isolation by its own unit
//     tests and hardhat contract tests.
//   - Only fullstack proves the WIRING: that the onlyRole modifier is
//     actually attached to stateUpdate, that the manager's account is the
//     grant target at construction, and that the revert selector surfaces
//     through the Go unpacker as expected.
//
// Design:
//   - Direct contract call from an unauthorised account. No executor or
//     manager needed — the role check fires before any payload validation,
//     so we can pass arbitrary bytes and still trigger the exact revert
//     we want.
//   - Fresh funded account via suite.CreateFundedAccount(): 5 ETH, zero
//     roles on the contract, no special setup required.
//   - The arbitrary UpdatePayload never reaches downstream checks (signature,
//     state-root, request ID) because the role modifier short-circuits first.
//
// Flow:
//
//  1. Start the suite (no executor/manager — saves ~5s; test is ~2s).
//  2. Create a fresh funded rogue account (5 ETH, no UPDATE_STATUS_ROLE).
//  3. Build a minimally-typed UpdatePayload with arbitrary field values.
//  4. suite.SubmitStateUpdateAs(rogueKey, payload) — direct contract call
//     signed by the rogue account, bypassing the manager and the wrapped
//     eventBroadcastingClient.
//  5. Assert the returned error contains "AccessControlUnauthorizedAccount"
//     (OZ's standard revert selector when the onlyRole modifier fails).
//  6. Cross-check: suite.AssertNoStateUpdateErrors(t) confirms the wrapped
//     eventBroadcastingClient's buffer is empty — verifies that
//     SubmitStateUpdateAs truly bypassed the wrapper (if a future
//     refactor accidentally routed through it, the buffer would now hold
//     an error and this assertion would flip the test red, catching the
//     rewiring regression).
func TestNonManagerStateUpdateReverts(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	// Step 1: suite start. No need to StartExecutor / StartManager — the
	// contract role check is purely on-chain and fires before any off-chain
	// processing is involved.
	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	// Step 2: fresh funded account, unambiguously unauthorised. Only the
	// manager account (set at ProcessorEndpoint construction via
	// updateStatusOperator) holds UPDATE_STATUS_ROLE.
	_, rogueKey, err := suite.CreateFundedAccount()
	require.NoError(t, err)

	// Step 3: arbitrary-but-typed payload. The role modifier reverts
	// before any field validation, so none of these values matter for
	// the assertion — we just need a struct that the Go binding accepts
	// when packing the calldata.
	update := &common.UpdatePayload{
		ApplicationID:  common.NewApplicationId(1),
		RequestID:      common.RequestIdType{},
		PrevStateRoot:  [32]byte{},
		NewStateRoot:   [32]byte{},
		Events:         []common.Event{},
		AppEvents:      []common.AppEvent{},
		Withdrawals:    []common.Withdrawal{},
		Signature:      []byte{},
		RefundAmount:   common.NewBig(0),
		ApplicationFee: common.NewBig(0),
		ErrorCode:      0,
		ErrorMsg:       "",
	}

	// Step 4 + 5: direct contract call signed by the rogue account.
	// Expect the OZ AccessControl revert.
	err = suite.SubmitStateUpdateAs(rogueKey, update)
	require.Error(t, err, "stateUpdate from non-manager account must be rejected")
	require.True(t,
		strings.Contains(err.Error(), "AccessControlUnauthorizedAccount"),
		"expected OZ AccessControlUnauthorizedAccount revert selector "+
			"(stateUpdate's onlyRole(UPDATE_STATUS_ROLE) gate); got: %v",
		err,
	)

	// Step 6: sanity cross-check. SubmitStateUpdateAs builds a fresh
	// blockchain.Client that does NOT go through the suite's wrapped
	// eventBroadcastingClient, so its stateUpdateErrors buffer should
	// stay empty. If a future refactor accidentally routed
	// SubmitStateUpdateAs through the wrapper, this assertion would
	// flip red — flagging the rewiring regression before it can
	// silently corrupt other negative-path tests that rely on the
	// buffer being a manager-only channel.
	suite.AssertNoStateUpdateErrors(t)

	t.Logf("stateUpdate correctly rejected with: %v", err)
}
