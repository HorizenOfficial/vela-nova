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

// TestFullStackRestart_KeysetRecovery proves that encrypted application
// state survives a coupled restart of BOTH the manager and the executor.
// This is the production-realistic reboot scenario: host reboots, Docker
// brings up both services under `restart: always`, and the system must
// resume cooperation with no data loss.
//
// The key invariant under test is the Type-0 keyset-recovery handshake
// working end-to-end against a populated data layer — i.e., the recovery
// blob written by the initial executor at first startup (into the manager's
// versioned LevelDB) is successfully served back to a fresh executor
// instance, which restores its original keyset and can decrypt state that
// was encrypted pre-restart.
//
// Four layers of robustness are exercised in one flow:
//
//  1. Manager's data layer persistence of the keyset-recovery blob across
//     the Manager instance's lifecycle (re-used struct pointer; the LevelDB
//     files are NOT deleted between manager instances).
//  2. Communication channel re-establishment — fresh comm.Server on the
//     same TCP port, fresh comm.Client from the rebuilt manager.
//  3. Executor's keyset restoration from the recovery blob (Type-0 handshake
//     messages 4 → 5 → 8 runs against the stored data, not against fresh
//     key generation).
//  4. Versioned LevelDB integrity: encrypted app state written pre-restart
//     is decrypted and read post-restart using the same recovered keyset.
//
// Flow (9 steps):
//
//  1. Start suite + executor + manager.
//  2. Driver deploys app + registers user (keyset-recovery blob is now
//     persisted in the manager's data layer).
//  3. Deposit 1 ETH. Assert private balance == 1 ETH — pre-restart state
//     is established and encrypted under the initial executor keyset.
//  4. suite.RestartAll() — stops both manager + executor, rebuilds fresh
//     instances over the preserved data layer, restarts. Fresh executor
//     runs the Type-0 handshake on startup; fresh manager serves the
//     stored recovery blob; executor restores its keyset.
//  5. Assert private balance STILL == 1 ETH. Proves the fresh executor
//     restored the same keyset (otherwise it could not decrypt the stored
//     state).
//  6. Deposit 0.5 ETH (new op post-restart). Exercises the full
//     post-recovery write path against the restored keyset.
//  7. Assert private balance == 1.5 ETH.
//  8. Withdraw 0.3 ETH (post-restart mutation path).
//  9. Assert private balance == 1.2 ETH and appCustody[ETH] == 1.2 ETH.
//
// Non-goals:
//   - Executor-only restart (the manager currently lacks reconnect logic;
//     coupled restart is the only meaningful test in the current design).
//   - Manager-only restart (symmetric same limitation).
//   - Multiple consecutive restarts (one round-trip proves the mechanism;
//     further iterations have diminishing value).
func TestFullStackRestart_KeysetRecovery(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	// Step 1: initial startup. The executor generates a keyset and (via the
	// first-time handshake) the manager stores the recovery blob in its
	// data layer. That stored blob is what the restart will recover from.
	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	driver := walletTestutil.NewWalletDriver(t, suite)

	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	// Step 2: app deploy + user registration. Registration submits the
	// AssociateKey request; the executor encrypts the resulting state with
	// the initial keyset.
	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID)
	require.NoError(t, driver.RegisterUser(t.Context(), "100 wei"))

	ethAddr := ethCommon.Address{}
	oneEther := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	halfEther := new(big.Int).Div(oneEther, big.NewInt(2))
	threeTenthsEther := new(big.Int).Mul(big.NewInt(3), new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil)) // 0.3 ETH

	// Step 3: deposit 1 ETH pre-restart. After this the encrypted app state
	// contains the deposit and is persisted in the manager's data layer.
	require.NoError(t, driver.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	balPre, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balPre, "pre-restart balance should be visible")
	require.Equal(t, 0, oneEther.Cmp(balPre),
		"pre-restart balance should be 1 ETH; got %s wei", balPre.String())

	// Step 4: restart both sides. Data layer survives (same *LevelDB*
	// instance reference), so the recovery blob is available to the fresh
	// manager. Fresh executor's Type-0 handshake should find it and restore
	// the same keyset that encrypted the deposit above.
	require.NoError(t, suite.RestartAll(), "RestartAll should complete the Type-0 handshake and restore the original keyset")

	// Step 5: the critical assertion. If the fresh executor did NOT restore
	// the original keyset (e.g., it generated a new one), it could not
	// decrypt the state written by the pre-restart executor, and this read
	// would either fail or return garbage / nil.
	balRestored, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err, "reading private balance after restart must not error")
	require.NotNil(t, balRestored, "post-restart balance MUST be visible — if nil, the keyset was not restored and old state is unreadable")
	require.Equal(t, 0, oneEther.Cmp(balRestored),
		"post-restart balance should equal pre-restart value (1 ETH); got %s wei", balRestored.String())

	// Step 6: new deposit post-restart. Exercises the full write path with
	// the restored keyset: encrypt state under the restored comm key, sign
	// with the restored signing key, teeAuthenticator on-chain still
	// accepts because the signing address is unchanged.
	require.NoError(t, driver.Deposit(t.Context(), "0.5 ETH", "", "100 wei"))

	// Step 7: cumulative balance.
	wantBalAfterSecondDeposit := new(big.Int).Add(oneEther, halfEther) // 1.5 ETH
	balAfterSecondDeposit, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balAfterSecondDeposit)
	require.Equal(t, 0, wantBalAfterSecondDeposit.Cmp(balAfterSecondDeposit),
		"post-restart + second-deposit balance should be 1.5 ETH; got %s wei", balAfterSecondDeposit.String())

	// Step 8: withdraw post-restart. Exercises a state-mutation path
	// (read + debit + write) plus on-chain pendingClaims credit, all via
	// the restored keyset.
	userHex := driver.UserAddress().Hex()
	require.NoError(t, driver.Withdraw(t.Context(), "0.3 ETH", userHex, "", "100 wei"))

	// Step 9: final state. Private balance reflects the debit; on-chain
	// custody reflects deposits minus the withdrawn-to-pendingClaims amount.
	wantBalFinal := new(big.Int).Sub(wantBalAfterSecondDeposit, threeTenthsEther) // 1.2 ETH
	balFinal, err := driver.GetPrivateBalance(t.Context(), "")
	require.NoError(t, err)
	require.NotNil(t, balFinal)
	require.Equal(t, 0, wantBalFinal.Cmp(balFinal),
		"final private balance should be 1.2 ETH; got %s wei", balFinal.String())

	custody := suite.GetAppCustody(appID, ethAddr)
	require.Equal(t, 0, wantBalFinal.Cmp(custody),
		"final on-chain custody should be 1.2 ETH (deposits minus withdrawn amount); got %s", custody.String())

	suite.AssertNoStateUpdateErrors(t)
	t.Log("Full-stack restart + keyset recovery: pre-restart state readable post-restart; new operations succeed against the restored keyset")
}
