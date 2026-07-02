package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestTEEAttestationRejection proves that ProcessorEndpoint's on-chain
// stateUpdate rejects a state update signed by a key that does NOT match
// the TEE signer address registered in the TeeAuthenticator contract. This
// is the security invariant underpinning the entire trust model — if the
// signature check were ever removed or short-circuited, a compromised
// manager could fabricate state updates without enclave authorisation, and
// the contract would accept them.
//
// Every other fullstack test deploys the mock authenticator pre-populated
// with the real executor's signing address, so the verify step trivially
// passes — a regression here would be invisible to those tests. This one
// flips the registered signer to a ROGUE address while leaving the
// executor's real signing key in place; the executor still signs state
// updates with its real key, those signatures recover to its real address,
// and the authenticator rejects them because that address != rogue.
//
// Fullstack-unique:
//   - Hardhat unit tests cover the TeeAuthenticator contract in isolation.
//   - Only fullstack proves the WIRING: manager actually calls stateUpdate,
//     ProcessorEndpoint actually delegates to the authenticator, a signer
//     mismatch actually surfaces as an on-chain revert visible to the
//     manager's submission path.
//
// Design choices:
//   - Rogue address registered at deploy time; executor keyset untouched.
//     Simpler than tampering with a signature post-sign, and still exactly
//     hits the verify branch we want to test.
//   - Real comm pubkey kept on-chain. If we rogued that too, wallet
//     payload encryption would fail at RegisterUser (well before reaching
//     the signature check) — a false positive that wouldn't prove anything
//     about the signature path.
//   - DeployApp is the trigger: a single request round-trip produces a
//     single stateUpdate submission. The revert manifests on that
//     submission. The same revert would hit any other request type
//     (deposit, withdraw, transfer) — all of them go through the same
//     stateUpdate entry point.
//
// Flow (6 steps):
//
//  1. Generate a rogue secp256k1 key; compute its address.
//  2. Build the suite via NewFullStackSystemTestSuiteWithTeeSigner with
//     the rogue address as the override. Start manager + executor.
//  3. Build the wallet driver (funded user + DEPLOYER_ROLE).
//  4. Kick off DeployApp in a goroutine — it will hang on the subgraph
//     poll because the stateUpdate never completes. This is fine; the
//     assertion doesn't wait on the driver, it waits on the underlying
//     state-update error channel.
//  5. suite.WaitForStateUpdateError(...) returns the specific error
//     (with a short budget — the revert reason should surface within a
//     few seconds of the executor processing the deploy).
//  6. Assert the error string contains "InvalidSignature" — the custom
//     error the contract reverts with when teeAuthenticator.checkSignature
//     returns false.
func TestTEEAttestationRejection(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	// Step 1: rogue signer address. Distinct from whatever the executor
	// generates; the registered signer on-chain will be this address, but
	// the executor will sign with its own real key.
	rogueKey, err := ethCrypto.GenerateKey()
	require.NoError(t, err)
	rogueAddr := ethCrypto.PubkeyToAddress(rogueKey.PublicKey)

	// Step 2: suite wiring with the rogue override.
	suite := fullstack.NewFullStackSystemTestSuiteWithTeeSigner(
		t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig(),
		&rogueAddr,
	)
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 3: driver (funded user with DEPLOYER_ROLE).
	driver := walletTestutil.NewWalletDriver(t, suite)

	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	// Step 4: kick off the deploy in a background goroutine. It will
	// never complete (subgraph never sees RequestCompleted because the
	// stateUpdate keeps reverting), so we don't wait on its result —
	// we assert on the captured state-update error instead.
	deployDone := make(chan error, 1)
	go func() {
		_, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
		deployDone <- err
	}()

	// Step 5 + 6: wait for the specific revert. The manager submits the
	// stateUpdate once the executor finishes processing the deploy
	// request, which happens within a few seconds of the on-chain
	// DeployRequest submission.
	stateUpdateErr, arrived := suite.WaitForStateUpdateError(30 * time.Second)
	require.True(t, arrived, "no state-update error arrived within 30s — the manager did not attempt stateUpdate, or the channel was full (unlikely with buffer=16)")
	require.Error(t, stateUpdateErr)
	require.True(t,
		strings.Contains(stateUpdateErr.Error(), "InvalidSignature"),
		"expected on-chain revert reason to mention InvalidSignature "+
			"(ProcessorEndpoint's custom error when teeAuthenticator.checkSignature returns false); got: %v",
		stateUpdateErr,
	)

	t.Logf("stateUpdate correctly rejected with: %v", stateUpdateErr)
	// The goroutine is still blocked on the driver's polling timeout.
	// Let it continue to exit cleanly on suite.Cleanup cancellation.
	_ = deployDone
}
