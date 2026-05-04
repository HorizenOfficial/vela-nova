// Package testutil provides WalletDriver, an in-process harness that drives
// vela-nova wallet commands against a FullStackSystemTestSuite.
//
// Configuration follows a "hybrid" pattern: the driver writes a real temp
// wallet.conf via app.SaveConfigToFile, then calls app.LoadConfigFromFile so
// the production config-loading path is exercised on every run. The only
// deliberate departure is subgraph injection: the loaded config's SubgraphURL
// is empty, so the driver assigns suite.GetSubgraph() directly onto each
// constructed ChainCommand (the SubgraphClient field is public).
//
// Unlike the suite's own wrapped blockchain client (bound to the manager's
// key), the driver builds its own blockchain.Client bound to the user's
// secp256k1 key, so transactions submitted by the wallet are signed by the
// user. During construction it also grants the user the ProcessorEndpoint
// DEPLOYER_ROLE so wallet-driven DeployApp works end-to-end.
//
// Typed wrapper methods (DeployApp, RegisterUser, Deposit, ...) set
// flag-bound struct fields via cobra.Command.Flags().Set() — this exercises
// Cobra's flag parsing — then call each command's exported Exec(ctx) method
// for the business logic. Errors propagate to the caller so tests can assert.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	ethCommon "github.com/ethereum/go-ethereum/common"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// unreachableRpcURL is a placeholder required by LoadConfigFromFile's URL
// parser. The driver never dials it — the blockchain.Client is injected
// pre-connected from the suite.
const unreachableRpcURL = "http://127.0.0.1:0"

// WalletDriver drives vela-nova wallet commands end-to-end against a
// FullStackSystemTestSuite. It owns a temp wallet.conf and a user identity
// (secp256k1 for on-chain, P521 for private-state encryption). A fresh
// user-signed blockchain.Client is built per wrapper invocation (commands'
// defer-Close() would otherwise kill a shared client after the first call).
type WalletDriver struct {
	t        *testing.T
	suite    *fullstack.FullStackSystemTestSuite
	confPath string
	userAddr ethCommon.Address // cached for per-call client construction
}

// NewWalletDriver constructs a WalletDriver for a fresh user. It generates a
// funded secp256k1 key via the suite, generates a P521 key, writes a temp
// wallet.conf populated from the suite's addresses, and re-loads it through
// the production LoadConfigFromFile path. Also grants the user the
// ProcessorEndpoint DEPLOYER_ROLE.
func NewWalletDriver(t *testing.T, suite *fullstack.FullStackSystemTestSuite) *WalletDriver {
	t.Helper()

	_, secpKey, err := suite.CreateFundedAccount()
	require.NoError(t, err)

	p521Key, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	return NewWalletDriverWithKeys(t, suite, secpKey, p521Key)
}

// NewWalletDriverNoDeployerRole is like NewWalletDriver but SKIPS the
// DEPLOYER_ROLE grant. Use it for tests that want to assert the contract-side
// role check — a driver built via this constructor will have its DeployApp
// calls reverted with DeployerNotAllowed until the caller subsequently calls
// suite.GrantDeployerRole on the driver's address.
func NewWalletDriverNoDeployerRole(t *testing.T, suite *fullstack.FullStackSystemTestSuite) *WalletDriver {
	t.Helper()

	_, secpKey, err := suite.CreateFundedAccount()
	require.NoError(t, err)

	p521Key, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	return newWalletDriverInternal(t, suite, secpKey, p521Key, false)
}

// NewWalletDriverWithKeys is like NewWalletDriver but uses caller-supplied
// keys. The user's secp key is registered with the suite (funded with ETH,
// TransactOpts stored) and granted DEPLOYER_ROLE on the ProcessorEndpoint.
func NewWalletDriverWithKeys(
	t *testing.T,
	suite *fullstack.FullStackSystemTestSuite,
	secpKey *cryptotypes.PrivateKeySecp256k1,
	p521Key *cryptotypes.PrivateKeyP521,
) *WalletDriver {
	t.Helper()
	return newWalletDriverInternal(t, suite, secpKey, p521Key, true)
}

// newWalletDriverInternal is the shared body for the public constructors.
// grantDeployerRole gates the role grant so test variants can build a driver
// whose deploys will be rejected on-chain.
func newWalletDriverInternal(
	t *testing.T,
	suite *fullstack.FullStackSystemTestSuite,
	secpKey *cryptotypes.PrivateKeySecp256k1,
	p521Key *cryptotypes.PrivateKeyP521,
	grantDeployerRole bool,
) *WalletDriver {
	t.Helper()

	// Ensure the user account is funded and has a TransactOpts registered with
	// the suite. Skip if already registered (e.g. via NewWalletDriver →
	// CreateFundedAccount) — a second RegisterAccount would re-fund 5 ETH and
	// can overrun the deployer's balance.
	userAddr := ethCrypto.PubkeyToAddress(secpKey.PrivateKey.PublicKey)
	if !suite.HasAccount(userAddr) {
		suite.RegisterAccount(secpKey.PrivateKey)
	}

	if grantDeployerRole {
		// Grant DEPLOYER_ROLE so wallet-driven DeployApp can submit deploy requests.
		suite.GrantDeployerRole(userAddr)
	}

	sim := suite.GetSimTestHelper()
	processor := sim.ProcessorContractAddress
	teeAuth := sim.TeeSignerAddress

	// Seed the registry with just the implicit ETH entry. ERC-20 tokens are
	// added later via WalletDriver.AddToken, which appends token.* entries
	// to the on-disk conf; the next loadConfig() call rebuilds the registry
	// from disk.
	tokens, err := app.LoadTokenRegistry(nil)
	require.NoError(t, err)

	cfg := &app.Config{
		KeySecp:                   secpKey,
		KeyP521:                   p521Key,
		RpcUrl:                    unreachableRpcURL,
		ProcessorEndpointAddress:  &processor,
		TeeAuthenticatorAddress:   &teeAuth,
		AuthorityServiceURL:       suite.GetAuthorityServiceURL(),
		SubgraphURL:               "", // injected in-process; no HTTP shim
		ApplicationID:             0,  // set by DeployApp
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  60,
		PrivateBalanceScanDepth:   200,
		Tokens:                    tokens,
	}

	confPath := filepath.Join(t.TempDir(), app.ConfFileName)
	require.NoError(t, app.SaveConfigToFile(cfg, confPath))

	// Exercise the production load path to catch conf/file drift.
	if _, err := app.LoadConfigFromFile(confPath); err != nil {
		t.Fatalf("wallet driver: initial LoadConfigFromFile failed: %v", err)
	}

	return &WalletDriver{
		t:        t,
		suite:    suite,
		confPath: confPath,
		userAddr: userAddr,
	}
}

// newUserClient builds a fresh user-signed blockchain.Client. Commands
// defer-Close() their BlockchainClient, so we can't share one across calls —
// each wrapper gets its own pre-connected client bound to the user's key.
func (d *WalletDriver) newUserClient() blockchain.Client {
	d.t.Helper()
	sim := d.suite.GetSimTestHelper()
	userOpts, err := d.suite.GetTransactOpts(d.userAddr)
	require.NoError(d.t, err)
	return blockchain.SetupNewBlockChainClientConnected(
		sim.Client(), sim.ProcessorContractAddress, sim.TeeSignerAddress, userOpts,
	)
}

// loadConfig re-reads the temp conf file each call. Fresh loads pick up
// ApplicationID persisted by DeployApp's SaveApplicationID, mirroring how a
// real CLI session picks up state between invocations.
func (d *WalletDriver) loadConfig() *app.Config {
	d.t.Helper()
	cfg, err := app.LoadConfigFromFile(d.confPath)
	require.NoError(d.t, err)
	return cfg
}

// UserAddress returns the secp256k1-derived address of the wallet's user.
func (d *WalletDriver) UserAddress() ethCommon.Address {
	return ethCommon.HexToAddress(d.loadConfig().KeySecp.PublicKey().Address())
}

// ApplicationID returns the currently configured application ID (0 before DeployApp).
func (d *WalletDriver) ApplicationID() common.ApplicationIdType {
	return d.loadConfig().ApplicationID
}

// ConfPath returns the path of the temp wallet.conf. Useful for tests that
// need to inspect the file between commands.
func (d *WalletDriver) ConfPath() string {
	return d.confPath
}

// NewBlockchainClient returns a fresh user-bound blockchain.Client. Exposed
// so tests can make direct on-chain queries (e.g. public balance) against
// the same chain view the wallet uses. Each call returns a new pre-connected
// client — the caller is responsible for Close() when done.
func (d *WalletDriver) NewBlockchainClient() blockchain.Client {
	return d.newUserClient()
}

// SetApplicationID writes the given application ID into the temp wallet.conf.
// Tests that want a second driver (e.g. recipient of a private transfer) to
// operate on an app deployed by a different driver call this after the first
// driver's DeployApp returns. Matches what the CLI would do: SaveApplicationID
// mutates the conf; subsequent loadConfig() picks it up.
func (d *WalletDriver) SetApplicationID(appID common.ApplicationIdType) {
	d.t.Helper()
	require.NoError(d.t, app.SaveApplicationID(d.confPath, appID))
}

// AddToken appends an ERC-20 entry to the wallet.conf file so subsequent
// wrapper calls can resolve the symbol (e.g. Deposit(..., "MOCK", ...)). The
// driver does not mutate an in-memory registry — each wrapper re-loads the
// conf via LoadConfigFromFile, which rebuilds the registry from the current
// file contents through LoadTokenRegistry (the same path a real CLI takes).
//
// Symbols are upper-cased on write, matching how the config parser stores
// and looks them up. The zero address is rejected by LoadTokenRegistry (it
// is reserved for ETH); surface that as an immediate error so tests don't
// fail in a downstream wrapper with a confusing message.
func (d *WalletDriver) AddToken(symbol string, address ethCommon.Address, decimals uint8) {
	d.t.Helper()
	require.NotEmpty(d.t, symbol, "AddToken: symbol must not be empty")
	require.NotEqual(d.t, velacommon.ETH_TOKEN, address, "AddToken: zero address is reserved for ETH")

	upperSym := strings.ToUpper(symbol)
	entry := fmt.Sprintf("token.%s.address=%s\ntoken.%s.decimals=%d\n", upperSym, address.Hex(), upperSym, decimals)

	f, err := os.OpenFile(d.confPath, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(d.t, err, "AddToken: open conf")
	defer f.Close()
	_, err = f.WriteString(entry)
	require.NoError(d.t, err, "AddToken: append conf")

	// Re-load now so any malformed entry fails here instead of during the
	// next wrapper call. Also catches collisions (duplicate symbol or address)
	// that LoadTokenRegistry would otherwise reject mid-flow.
	_, err = app.LoadConfigFromFile(d.confPath)
	require.NoError(d.t, err, "AddToken: re-load conf after append")
}
