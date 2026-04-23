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

// TestPrivateTransfer_TwoUsers_ERC20 is the ERC-20 + multi-token sibling of
// TestPrivateTransfer_TwoUsers. Beyond re-running the two-user private
// transfer flow with an ERC-20 asset, it pins down three things that the
// ETH-only test and the single-token TestERC20FullStack cannot catch alone:
//
//  1. parseAssetAmount correctness for non-18 decimals. MOCK is deployed with
//     decimals=6 (USDC-like). The wallet must interpret "1000" as 1000*10^6
//     raw, not 1000*10^18 — any hardcoded 18-decimal assumption on the ERC-20
//     path would blow up here.
//
//  2. Per-token balance isolation on a single user. User A privately holds
//     both 1 ETH and 1000 MOCK. After A transfers 300 MOCK to B, A's MOCK
//     balance reads 700 (in raw 10^6 units) and A's ETH balance reads 1 ETH
//     UNCHANGED. If getprivatebalance's tokenAddress filter (or the
//     wallet-side decrypt) cross-contaminated across tokens, this assertion
//     would fail.
//
//  3. Per-user event routing for ERC-20 events. B sees 300 MOCK; B's ETH
//     reads nil (B never received ETH, even though A holds some). This
//     extends the ETH-only routing check to events carrying a non-zero
//     tokenAddress.
//
// Flow (12 steps):
//
//  1. Suite start; deploy MockERC20("MOCK", decimals=6); allowlist on
//     ProcessorEndpoint; start executor + manager.
//  2. Two drivers A, B; both AddToken("MOCK", mockAddr, 6).
//  3. A deploys the payment-app with MOCK in allowedTokens; B.SetApplicationID(appID).
//  4. Both RegisterUser.
//  5. Mint 10,000 MOCK to A; pre-approve processor for 10,000 MOCK (option-A
//     pattern — wallet does not embed EIP-2612 permits).
//  6. A.Deposit("1000", "MOCK") → assert appCustody[MOCK] == 1000*10^6.
//  7. A.Deposit("1 ETH", "") → gives A a parallel ETH balance.
//  8. A.GetPrivateBalance("MOCK") == 1000*10^6 and A.GetPrivateBalance("") == 1 ETH.
//  9. A.PrivateTransfer(B, "300", "MOCK").
// 10. A.GetPrivateBalance("MOCK") == 700*10^6 AND A.GetPrivateBalance("") == 1 ETH
//     (the multi-token isolation assertion — ETH untouched by the MOCK transfer).
// 11. B.GetPrivateBalance("MOCK") == 300*10^6.
// 12. B.GetPrivateBalance("") == nil (B never received ETH).
func TestPrivateTransfer_TwoUsers_ERC20(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	sim := suite.GetSimTestHelper()

	// Step 1: 6-decimal MOCK is the whole point of this variant — USDC-like,
	// catches any 10^18 assumption baked into the wallet ERC-20 path.
	const mockDecimals = 6
	mockAddr := sim.DeployMockERC20("Mock Token", "MOCK", mockDecimals)
	sim.WaitMined(sim.AddAllowedToken(mockAddr))

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 2: two drivers; both learn about MOCK so either can resolve the
	// symbol on deposit / transfer / balance lookups.
	driverA := walletTestutil.NewWalletDriver(t, suite)
	driverB := walletTestutil.NewWalletDriver(t, suite)
	driverA.AddToken("MOCK", mockAddr, mockDecimals)
	driverB.AddToken("MOCK", mockAddr, mockDecimals)

	// Step 3: A deploys with MOCK allowlisted at the guest level.
	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driverA.DeployApp(t.Context(), wasmPath, "100 wei", []string{"MOCK"})
	require.NoError(t, err)
	require.NotZero(t, appID)
	driverB.SetApplicationID(appID)

	// Step 4: both register — seeds the per-user event routing.
	require.NoError(t, driverA.RegisterUser(t.Context(), "100 wei"))
	require.NoError(t, driverB.RegisterUser(t.Context(), "100 wei"))

	// Step 5: mint MOCK to A and pre-approve the processor. Option-A pattern
	// (permit-embedding wallet is future work).
	mockUnit := new(big.Int).Exp(big.NewInt(10), big.NewInt(mockDecimals), nil) // 10^6
	mintAmount := new(big.Int).Mul(big.NewInt(10000), mockUnit)
	userA := driverA.UserAddress()
	sim.WaitMined(sim.MintERC20(userA, mintAmount))

	userAOpts, err := suite.GetTransactOpts(userA)
	require.NoError(t, err)
	sim.WaitMined(sim.ApproveERC20(userAOpts, sim.ProcessorContractAddress, mintAmount))

	// Step 6: deposit 1000 MOCK. The wallet parses "1000" with decimals=6
	// from the registry entry we just added.
	depositMockRaw := new(big.Int).Mul(big.NewInt(1000), mockUnit)
	require.NoError(t, driverA.Deposit(t.Context(), "1000", "MOCK", "100 wei"))
	require.Equal(t, 0, depositMockRaw.Cmp(suite.GetAppCustody(appID, mockAddr)),
		"appCustody[MOCK] should equal 1000 MOCK after deposit")

	// Step 7: deposit 1 ETH. A now holds two assets privately.
	oneEther := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	require.NoError(t, driverA.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	// Step 8: both balances visible, each in its own unit.
	balAMock, err := driverA.GetPrivateBalance(t.Context(), "MOCK")
	require.NoError(t, err)
	require.NotNil(t, balAMock, "A should see a MOCK balance after deposit")
	require.Equal(t, 0, depositMockRaw.Cmp(balAMock),
		"A's MOCK balance should be 1000 MOCK (raw %s); got %s", depositMockRaw.String(), balAMock.String())

	balAEth, err := driverA.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balAEth, "A should see an ETH balance after deposit")
	require.Equal(t, 0, oneEther.Cmp(balAEth),
		"A's ETH balance should be 1 ETH; got %s wei", balAEth.String())

	// Step 9: private-transfer 300 MOCK to B. Only touches encrypted state.
	require.NoError(t, driverA.PrivateTransfer(t.Context(),
		driverB.UserAddress().Hex(), "300", "MOCK", "100 wei"))

	// Step 10: A's MOCK decreased; A's ETH MUST be unchanged. This is the
	// multi-token isolation check — if getprivatebalance's tokenAddress
	// filter is broken (or the wallet's event decrypt mis-routes), the ETH
	// read would reflect the MOCK transfer (or return a stale/wrong value).
	transferredMock := new(big.Int).Mul(big.NewInt(300), mockUnit)
	remainingMock := new(big.Int).Sub(depositMockRaw, transferredMock)

	balAMock2, err := driverA.GetPrivateBalance(t.Context(), "MOCK")
	require.NoError(t, err)
	require.NotNil(t, balAMock2)
	require.Equal(t, 0, remainingMock.Cmp(balAMock2),
		"A's post-transfer MOCK balance should be 700 MOCK; got %s", balAMock2.String())

	balAEth2, err := driverA.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balAEth2, "A's ETH balance should still be visible after MOCK transfer")
	require.Equal(t, 0, oneEther.Cmp(balAEth2),
		"A's ETH balance should be UNCHANGED at 1 ETH after a MOCK-only transfer; got %s wei", balAEth2.String())

	// Step 11: B sees the 300 MOCK — proves ERC-20 event routing reaches
	// the recipient correctly.
	balBMock, err := driverB.GetPrivateBalance(t.Context(), "MOCK")
	require.NoError(t, err)
	require.NotNil(t, balBMock, "B should see a MOCK balance after receiving the transfer")
	require.Equal(t, 0, transferredMock.Cmp(balBMock),
		"B's MOCK balance should be 300 MOCK; got %s", balBMock.String())

	// Step 12: B must NOT see any ETH — A holds ETH but never sent any to B.
	// If cross-user filtering leaked A's ETH events into B's view, this nil
	// check would fail.
	balBEth, err := driverB.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.Nil(t, balBEth, "B should have no ETH private balance — A holds ETH but never transferred any to B")

	t.Log("ERC-20 private transfer with multi-token isolation verified: non-18 decimals, per-token filter, per-user routing")
}
