package main_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	walletTestutil "github.com/HorizenOfficial/vela-nova/wallet/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	"github.com/stretchr/testify/require"
)

// TestDeanonymize_RealRoundTrip exercises the full authority deanonymization
// pipeline against real components — no synthetic injection. It is the
// end-to-end counterpart to TestAuthorityServiceGetReportRoundTrip (in the
// vela fullstack package), which only verifies the HTTP service boot and
// signature check against a hand-written report file.
//
// Flow:
//  1. Two wallet drivers: user (deposits ETH so there's something to report)
//     and auth (acts as the authority — registered user + on-chain authority).
//  2. Both RegisterUser so the executor has both P521 pubkeys in its
//     keyStore (required for encrypting the report to auth).
//  3. suite.RegisterAuthority grants auth's address DEFAULT_AUTHORITY role
//     for the app — otherwise the on-chain Deanonymize submit reverts.
//  4. auth.RequestReport submits a Deanonymize request. The manager picks
//     it up, the WASM guest returns report bytes, the executor encrypts
//     those bytes to auth's P521 pubkey, and the manager writes the resulting
//     DeanonymizationReport JSON to the reports dir.
//  5. auth.DownloadReport runs the /nonce + /getreport + decrypt flow
//     against the in-process authority service, writing the decrypted
//     report to a destination file.
//  6. The test reads that file and asserts the decrypted content carries
//     both the expected ApplicationID/RequestID AND a non-empty reportData
//     payload that references the user's address. This is what catches
//     regressions in any layer — subgraph indexing of the RequestCompleted
//     event, real executor-side encryption-to-requester, or HTTP boundary
//     signature verification.
func TestDeanonymize_RealRoundTrip(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment",
		testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	driverUser := walletTestutil.NewWalletDriver(t, suite)
	driverAuth := walletTestutil.NewWalletDriver(t, suite)

	wasmBytes := testhelpers.BuildAndLoadWasmModule(t)
	wasmPath := filepath.Join(t.TempDir(), "payment_app.wasm")
	require.NoError(t, os.WriteFile(wasmPath, wasmBytes, 0o644))

	appID, err := driverUser.DeployApp(t.Context(), wasmPath, "100 wei", nil)
	require.NoError(t, err)
	require.NotZero(t, appID)
	driverAuth.SetApplicationID(appID)

	// Both wallets register so the executor keyStore has their P521 pubkeys.
	// Without auth's P521 entry, the executor's
	// encryptDeanonymizationReport call would fail with
	// CodePubKeyNotRegistered.
	require.NoError(t, driverUser.RegisterUser(t.Context(), "100 wei"))
	require.NoError(t, driverAuth.RegisterUser(t.Context(), "100 wei"))

	// User has some balance to report on.
	require.NoError(t, driverUser.Deposit(t.Context(), "1 ETH", "", "100 wei"))

	// Grant auth's on-chain address the DEFAULT_AUTHORITY role for this
	// application — otherwise the Deanonymize submitRequest reverts with
	// AuthorityNotAllowed.
	suite.RegisterAuthority(appID)
	// suite.RegisterAuthority grants the *in-process authority* (the key
	// baked into the fullstack suite's InProcessAuthority) the
	// default-authority role, but our driverAuth is a different address.
	// Grant it too via the lower-level helper.
	sim := suite.GetSimTestHelper()
	sim.WaitMined(sim.AddAuthority(new(big.Int).SetUint64(uint64(appID)), driverAuth.UserAddress()))

	// Auth submits the Deanonymize request. Default report type = "balances".
	reportID, err := driverAuth.RequestReport(t.Context(), "balances", "100 wei")
	require.NoError(t, err)

	// Report is now persisted to the manager's reports dir (the same path
	// the in-process authority service reads from — configured at suite
	// setup) and the subgraph has the RequestCompleted entry for the
	// /getreport endpoint's sanity check. Pull it down via the full HTTP +
	// decrypt pipeline.
	reportIDHex := fmt.Sprintf("%x", reportID[:])
	t.Logf("deanonymization request id: %s", reportIDHex)
	destPath := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, driverAuth.DownloadReport(t.Context(), reportIDHex, destPath))

	raw, err := os.ReadFile(destPath)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	// The decrypted report's shape is what downloadreport writes:
	// applicationId and requestId are string-encoded (their custom
	// MarshalJSON), reportData is the raw payload (JSON or base64 fallback).
	var got struct {
		ApplicationID string          `json:"applicationId"`
		RequestID     string          `json:"requestId"`
		ReportData    json.RawMessage `json:"reportData"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Equal(t, appID.String(), got.ApplicationID,
		"decrypted report should reference the deployed app")
	// RequestIdType.MarshalJSON adds "0x" prefix; our reportIDHex is the
	// unprefixed form used as the downloadreport flag value.
	require.Equal(t, "0x"+reportIDHex, got.RequestID,
		"decrypted report should reference the request that produced it")
	require.NotEmpty(t, got.ReportData, "reportData should be populated for a balances report")

	// Balances reports include the user's address somewhere in the payload.
	// Compare case-insensitively — the payment app emits lowercase hex while
	// UserAddress() returns the EIP-55 checksum form.
	require.Contains(t, strings.ToLower(string(got.ReportData)),
		strings.ToLower(driverUser.UserAddress().Hex()),
		"balances report should mention the depositing user; raw=%s", string(got.ReportData))

	suite.AssertNoStateUpdateErrors(t)
	t.Log("Real deanonymize /getreport round-trip verified")
}
