package main_test

import (
	"context"
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

// TestFeeRefundClaim proves the fee-refund mechanism works end-to-end:
// users who submit maxFeeValue above the executor's actual applicationFee
// get the difference credited to pendingClaims[ETH][user] and can claim
// it. A regression anywhere along this chain — the executor's fee
// computation, the contract's pendingClaims accumulator, the claim
// payout — would silently cost users money on every request.
//
// ## What's being exercised
//
// Three layers cooperate:
//
//  1. Executor-side fee computation (pkg/executor/executor.go):
//       applicationFee = max(fuel × FuelPricePerUnit, MinFeePerRequest)
//       refundAmount   = maxFeeValue − applicationFee
//     Both values go into the signed UpdatePayload.
//
//  2. Contract-side accumulator (ProcessorEndpoint.sol, the success branch
//     of stateUpdate):
//       if (refund > 0) _asyncTransfer(ETH_TOKEN, feeRecipient, refund);
//     → pendingClaims[ETH][user] += refund on each successful request.
//
//  3. Claim payout (ProcessorEndpoint.claim):
//       transfers pendingClaims[token][payee] to payee on-chain and
//       zeroes the accumulator.
//
// ## Fullstack-unique
//
//   - Mocks stub the fee accounting entirely — no applicationFee
//     computation, no pendingClaims accumulator, no claim flow.
//   - Only fullstack exercises the full chain: real fuel costs inside a
//     real Wasmtime runtime → real executor arithmetic → real on-chain
//     UpdatePayload fields → real contract-side accumulator → real
//     on-chain payout.
//
// ## Claim-path choice
//
// Two ways to drive the claim:
//
//   - driver.ClaimPendingPayments(ctx, "") — the wallet CLI path. Fully
//     realistic but the user pays gas in ETH, so the user's balance delta
//     is (refund − gasCost). Asserting this exactly requires extracting
//     the tx receipt from the driver, which the current wrapper doesn't
//     expose.
//   - sim.Claim(ETH_TOKEN, user) — the contract's claim is public; anyone
//     can call it on the user's behalf and the funds still go to the
//     payee. The deployer account pays gas here, so the user's balance
//     delta is exactly the refund.
//
// We use sim.Claim for the exact-wei balance assertion. The wallet's
// ClaimPendingPayments command is already exercised by TestERC20FullStack
// (for MOCK tokens); this test's job is to pin the fee-accumulator
// invariant to the wei, and the clean math makes that possible.
//
// ## Flow
//
//  1. Suite start + executor + manager. Build driver.
//  2. Three happy-path requests (deploy + register + deposit), each with
//     maxFeeValue = 100 wei. Each successful stateUpdate credits some
//     refund to pendingClaims[ETH][driver]. Three requests is enough to
//     make the accumulator unambiguously non-trivial.
//  3. Capture refundTotal = sim.GetPendingClaims(ETH, user) — must be > 0.
//  4. Capture balanceBefore = BalanceAt(user).
//  5. sim.Claim(ETH_TOKEN, user) — deployer pays gas, user receives funds.
//  6. Assertions:
//       a. balanceAfter == balanceBefore + refundTotal  (exact; no gas on user side)
//       b. pendingClaims[ETH][user] == 0               (accumulator cleared)
//  7. AssertNoStateUpdateErrors — pure happy-path flow; the wrapper's
//     error channel should be empty.
func TestFeeRefundClaim(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 2: three happy-path requests. Each carries maxFeeValue = 100 wei,
	// which is well above the executor's MinFeePerRequest (10 wei default),
	// so each produces a non-trivial refund.
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

	// Step 3: refunds accumulated across the three requests. If this is 0,
	// the accumulator is broken somewhere between the executor computing
	// refundAmount and the contract crediting pendingClaims.
	sim := suite.GetSimTestHelper()
	ethAddr := ethCommon.Address{}
	refundTotal := sim.GetPendingClaims(ethAddr, user)
	require.Equal(t, 1, refundTotal.Sign(),
		"pendingClaims[ETH][user] should be strictly positive after three successful requests with maxFee=100 wei; got %s",
		refundTotal.String())

	// Step 4: snapshot the user's on-chain balance before the claim. After
	// the claim we expect this to increase by exactly refundTotal (deployer
	// pays the gas in step 5).
	ctx := context.Background()
	balanceBefore, err := sim.Client().BalanceAt(ctx, user, nil)
	require.NoError(t, err)

	// Step 5: payout via the contract's public claim. Deployer account is
	// the msg.sender here (SimTestHelper.Claim wires that up); the payee
	// argument is the user, so funds land in user's on-chain balance
	// without any gas deduction from the user.
	sim.WaitMined(sim.Claim(ethAddr, user))

	// Step 6a: exact balance equality. If the contract paid out a different
	// amount than the accumulator said, this would fail. If the wrong
	// account received the funds, the user's balance wouldn't move at all.
	balanceAfter, err := sim.Client().BalanceAt(ctx, user, nil)
	require.NoError(t, err)
	expectedBalanceAfter := new(big.Int).Add(balanceBefore, refundTotal)
	require.Equal(t, 0, expectedBalanceAfter.Cmp(balanceAfter),
		"post-claim user ETH balance should increase by exactly refundTotal (%s wei); "+
			"before=%s, after=%s, delta=%s",
		refundTotal.String(),
		balanceBefore.String(),
		balanceAfter.String(),
		new(big.Int).Sub(balanceAfter, balanceBefore).String(),
	)

	// Step 6b: accumulator cleared. If this is still non-zero, the contract's
	// claim didn't zero the pendingClaims entry — would lead to double-claim
	// in the wild.
	require.Equal(t, 0, big.NewInt(0).Cmp(sim.GetPendingClaims(ethAddr, user)),
		"pendingClaims[ETH][user] should be zero after claim")

	// Step 7: pure happy-path test; nothing should have reverted on the
	// manager's stateUpdate path. A non-empty buffer would indicate an
	// unrelated regression interfering with the fee flow.
	suite.AssertNoStateUpdateErrors(t)

	t.Logf("Fee refund claim verified: accumulated %s wei across 3 requests, claimed cleanly", refundTotal.String())
}
