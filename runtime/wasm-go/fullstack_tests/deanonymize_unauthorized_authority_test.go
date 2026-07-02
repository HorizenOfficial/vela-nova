package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestDeanonymizeRequestFromUnauthorizedAuthorityReverts proves that
// ProcessorEndpoint.submitRequest rejects a DEANONYMIZATION request from
// an account that is NOT on the application's authority allowlist. This
// is the gate that keeps user data confidential — any loosening here
// would let arbitrary accounts trigger deanonymization reports on apps
// they have no business seeing into.
//
// ## Revert path
//
// ProcessorEndpoint.sol submitRequest:
//
//   } else if (requestType == Structs.RequestType.DEANONYMIZATION) {
//       if (!authorityRegistry.checkAuthorityIsAllowed(applicationId, msg.sender)) {
//           revert AuthorityNotAllowed();
//       }
//   }
//
// The check fires at submitRequest time — before the request ever enters
// the pending queue and before any manager/executor processing. A rogue
// authority's request never costs the protocol anything beyond the gas
// spent on the (reverting) submission.
//
// ## Fullstack-unique
//
//   - Hardhat unit tests cover AuthorityRegistry.checkAuthorityIsAllowed
//     in isolation against a unit-test contract.
//   - Only fullstack proves the wiring: that ProcessorEndpoint.submitRequest
//     actually consults the registry on the DEANONYMIZATION branch, that
//     msg.sender is the value compared against the allowlist, and that
//     the AuthorityNotAllowed selector surfaces through the Go unpacker.
//
// ## Design
//
//   - Single driver (fresh user). NewWalletDriver auto-grants DEPLOYER_ROLE
//     to let it deploy the app, but does NOT grant any authority role on
//     the app's AuthorityRegistry entry. The allowlist is default-empty
//     for a freshly-deployed app, so this driver is unambiguously
//     unauthorized as an authority.
//   - No call to suite.RegisterAuthority / sim.AddAuthority for this
//     driver. Skipping those is the whole point — TestDeanonymize_RealRoundTrip
//     is the happy-path counterpart that DOES grant the role; this is the
//     negative path that proves the gate actually fires without the grant.
//   - Caller registration (RegisterUser) is NOT required: the Deanonymize
//     payload is encrypted to the TEE's pubkey (read from the contract),
//     not to the caller's own P521 key. The authority check fires at the
//     contract level regardless of caller-side registration state.
//
// ## Flow
//
//  1. Suite start + executor + manager.
//  2. Build driver (fresh user with DEPLOYER_ROLE, no authority role).
//  3. driver.DeployApp — the ONLY reason to do this is to create an app
//     whose applicationStateRoots[appID] is non-zero; without this, the
//     submitRequest call would hit InvalidApplicationId first and the
//     test would be testing the wrong gate.
//  4. driver.RequestReport(...) — submits a DEANONYMIZATION request from
//     an unauthorized account. Contract reverts with AuthorityNotAllowed.
//     The wallet driver surfaces the error.
//  5. Assert the error string contains "AuthorityNotAllowed".
//  6. suite.AssertNoStateUpdateErrors(t) — the revert is at submitRequest
//     time, so the request never enters the pending queue and the manager
//     never attempts a stateUpdate for it. The defensive hook must stay
//     clean.
func TestDeanonymizeRequestFromUnauthorizedAuthorityReverts(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	// Step 2: fresh driver. DEPLOYER_ROLE auto-granted; authority role
	// explicitly NOT granted. This is the whole asymmetry.
	driver := walletTestutil.NewWalletDriver(t, suite)

	// Step 3: deploy so the app exists. Without this, submitRequest would
	// hit InvalidApplicationId first (applicationStateRoots[appID] == 0),
	// and the test would be testing the wrong gate.
	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driver.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID)

	// Step 4 + 5: unauthorized deanonymize request. The contract's
	// authorityRegistry.checkAuthorityIsAllowed returns false for this
	// driver's address (never granted), so submitRequest reverts with
	// AuthorityNotAllowed. The wallet driver surfaces the unpacker's
	// error string.
	_, err = driver.RequestReport(t.Context(), "balances", "100 wei")
	require.Error(t, err, "deanonymize request from unauthorized account must fail at submitRequest time")
	require.Contains(t, err.Error(), "AuthorityNotAllowed",
		"expected AuthorityNotAllowed revert (ProcessorEndpoint's DEANONYMIZATION branch check); got: %v", err,
	)

	// Step 6: no stateUpdate was attempted — the revert rejected the
	// request before it entered the pending queue, so the manager never
	// processed anything for it. The wrapper's error channel must stay
	// empty. A non-empty channel would indicate (a) the rejection leaked
	// into the stateUpdate path somehow, or (b) some unrelated update
	// hit an unexpected error during the preceding deploy.
	suite.AssertNoStateUpdateErrors(t)

	t.Logf("deanonymize request from unauthorized authority correctly rejected: %v", err)
}
