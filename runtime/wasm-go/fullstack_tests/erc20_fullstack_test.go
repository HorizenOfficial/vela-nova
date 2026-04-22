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

// TestERC20FullStack is the Phase 5 reference test: a full end-to-end ERC-20
// flow that exercises every layer of the harness (real simulated chain, real
// Wasmtime payment-app, in-process subgraph, in-process authority, wallet
// driver). The flow mirrors the plan's 14 steps:
//
//  1. Start the suite.
//  2. Deploy MockERC20.
//  3. Allowlist it on ProcessorEndpoint.
//  4. Start manager + executor.
//  5. Build wallet driver (creates funded user, grants DEPLOYER_ROLE).
//  6. driver.AddToken("MOCK", mockAddr, 18) — adds the token to wallet.conf.
//  7. driver.DeployApp(wasmPath, ["MOCK"]) — wallet-driven deploy.
//  8. driver.RegisterUser().
//  9. Mint MockERC20 to the user (test-side fixture).
// 10. suite.ApproveERC20(user -> processor, amount) — option A: pre-approve
//     before driver.Deposit, since the wallet's current submitRequest path
//     does not embed EIP-2612 permits.
// 11. driver.Deposit("1000", "MOCK"). Assert on-chain appCustody += 1000 MOCK.
// 12. driver.GetPrivateBalance("MOCK") → 1000 MOCK (in raw units).
// 13. driver.Withdraw("400", userPublicAddr, "MOCK").
// 14. Assert pendingClaims[MOCK][user] == 400 MOCK.
// 15. driver.ClaimPendingPayments("MOCK"). Assert user's on-chain MOCK balance
//     increased by 400.
// 16. driver.GetPrivateBalance("MOCK") → 600 MOCK remaining.
func TestERC20FullStack(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	sim := suite.GetSimTestHelper()

	// Step 2: deploy MockERC20. 18 decimals matches most real ERC-20s and
	// makes raw-unit math match wei conversions.
	mockAddr := sim.DeployMockERC20("Mock Token", "MOCK", 18)
	t.Logf("MockERC20 deployed at %s", mockAddr.Hex())

	// Step 3: allowlist on ProcessorEndpoint (ADMIN-role'd call from Deployer).
	sim.WaitMined(sim.AddAllowedToken(mockAddr))

	// Step 4: start executor + manager (order matters — manager handshakes with executor).
	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 5: wallet driver — creates funded user, grants DEPLOYER_ROLE.
	driver := walletTestutil.NewWalletDriver(t, suite)

	// Step 6: teach the wallet about MOCK by appending to wallet.conf.
	// Subsequent driver wrappers re-load the conf and resolve "MOCK" via
	// LoadTokenRegistry.
	driver.AddToken("MOCK", mockAddr, 18)

	// Step 7: deploy the payment-app WASM with MOCK allowlisted at the guest level
	// (ConstructorParams.allowedTokens). The wallet's deployapp resolves "MOCK"
	// via the registry entry we just added.
	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", []string{"MOCK"})
	require.NoError(t, err)
	require.NotZero(t, appID, "DeployApp must assign a non-zero ApplicationID")
	t.Logf("Deployed application %d", appID)

	// Step 8: register the user's P521 key so the enclave can encrypt events.
	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))

	// Step 9: mint 10,000 MOCK to the user (test-side fixture — MockERC20's mint
	// is permissionless).
	mockUnit := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 10^18
	mintAmount := new(big.Int).Mul(big.NewInt(10000), mockUnit)
	user := driver.UserAddress()
	sim.WaitMined(sim.MintERC20(user, mintAmount))
	require.Equal(t, 0, mintAmount.Cmp(sim.BalanceOfERC20(user)),
		"user should hold the minted MOCK before deposit")

	// Step 10: pre-approve the ProcessorEndpoint to spend the user's MOCK.
	// Option A of the Phase 5 flow — the wallet's submitRequest uses
	// transferFrom and does not embed an EIP-2612 permit, so approval must be
	// pre-staged. A permit-embedding wallet (option B) is future work.
	userOpts, err := suite.GetTransactOpts(user)
	require.NoError(t, err)
	sim.WaitMined(sim.ApproveERC20(userOpts, sim.ProcessorContractAddress, mintAmount))

	// Step 11: deposit 1000 MOCK. Amount is a decimal string (wallet parses
	// with token decimals).
	depositRaw := new(big.Int).Mul(big.NewInt(1000), mockUnit)
	require.NoError(t, driver.Deposit(t.Context(), "1000", "MOCK", "100 wei"))

	custody := suite.GetAppCustody(appID, mockAddr)
	require.Equal(t, 0, depositRaw.Cmp(custody),
		"on-chain appCustody should equal deposited amount (1000 MOCK); got %s", custody.String())

	// Step 12: the wallet's private balance view decrypts the user's deposit
	// events and reports the running balance in raw token units.
	privBal, err := driver.GetPrivateBalance(t.Context(), "MOCK")
	require.NoError(t, err)
	require.NotNil(t, privBal, "GetPrivateBalance returned nil (no matching event found)")
	require.Equal(t, 0, depositRaw.Cmp(privBal),
		"private balance after deposit should equal deposited amount; got %s", privBal.String())

	// Step 13: withdraw 400 MOCK back to the user's public address.
	withdrawRaw := new(big.Int).Mul(big.NewInt(400), mockUnit)
	require.NoError(t, driver.Withdraw(t.Context(), "400", user.Hex(), "MOCK", "100 wei"))

	// Step 14: pendingClaims accumulates the withdrawn amount until Claim is called.
	pending := sim.GetPendingClaims(mockAddr, user)
	require.Equal(t, 0, withdrawRaw.Cmp(pending),
		"pendingClaims[MOCK][user] should equal the withdraw amount; got %s", pending.String())

	// Step 15: claim moves the pending amount to the user's ERC-20 balance.
	// Before claim, the user holds (mint - deposit) MOCK on-chain. After claim,
	// that should increase by the withdrawn amount.
	balBeforeClaim := sim.BalanceOfERC20(user)
	expectedSpent := depositRaw // everything we deposited is off the user's on-chain balance
	require.Equal(t, 0, new(big.Int).Sub(mintAmount, expectedSpent).Cmp(balBeforeClaim),
		"pre-claim balance sanity check failed")

	require.NoError(t, driver.ClaimPendingPayments(t.Context(), "MOCK"))

	balAfterClaim := sim.BalanceOfERC20(user)
	expectedBalAfterClaim := new(big.Int).Add(balBeforeClaim, withdrawRaw)
	require.Equal(t, 0, expectedBalAfterClaim.Cmp(balAfterClaim),
		"post-claim balance should include the claimed withdraw; got %s", balAfterClaim.String())

	// pendingClaims should be zeroed after the claim.
	pendingAfter := sim.GetPendingClaims(mockAddr, user)
	require.Equal(t, 0, big.NewInt(0).Cmp(pendingAfter),
		"pendingClaims should be zero after claim; got %s", pendingAfter.String())

	// Step 16: private balance should now be (deposit - withdraw) = 600 MOCK.
	remainingRaw := new(big.Int).Sub(depositRaw, withdrawRaw)
	privBal2, err := driver.GetPrivateBalance(t.Context(), "MOCK")
	require.NoError(t, err)
	require.NotNil(t, privBal2)
	require.Equal(t, 0, remainingRaw.Cmp(privBal2),
		"remaining private balance should be 600 MOCK; got %s", privBal2.String())

	t.Log("Phase 5 ERC-20 full-stack flow passed")
}
