package cmd

import (
	"bytes"
	"io"
	"math/big"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/magiconair/properties"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetPublicBalanceCmd_ZeroBalance verifies the command prints "0 ETH" for
// a freshly-generated (unfunded) wallet key. Uses an injected simulated chain
// client — no live RPC dependency, unlike the previous version that dialled
// base-sepolia and failed on DNS flakes.
func TestGetPublicBalanceCmd_ZeroBalance(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = old }()

	testHelper := setupSimTestHelper(t, nil)
	defer testHelper.Close()

	key, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	cmd := NewGetPublicBalanceCommand(&app.Config{KeySecp: key})
	cmd.Client = testHelper.Client()
	require.NoError(t, cmd.Exec(t.Context()))

	w.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "0 ETH")
}

// TestGetPublicBalanceCmd_FundedBalance exercises the non-zero ETH formatting
// path by funding a fresh key on the simulated chain and then reading its
// public balance through the command. Complements the zero-balance test —
// together they cover both branches of the FormatAmount output.
func TestGetPublicBalanceCmd_FundedBalance(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = old }()

	testHelper := setupSimTestHelper(t, nil)
	defer testHelper.Close()

	key, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	userAddr := ethCommon.HexToAddress(key.PublicKey().Address())
	fundAmount := new(big.Int).Mul(big.NewInt(2), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	testHelper.WaitMined(testHelper.TransferFunds(testHelper.Deployer, userAddr, fundAmount))

	cmd := NewGetPublicBalanceCommand(&app.Config{KeySecp: key})
	cmd.Client = testHelper.Client()
	require.NoError(t, cmd.Exec(t.Context()))

	w.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "2 ETH")
}

// TestGetPublicBalanceCmd_ERC20_Balance exercises the ERC-20 balanceOf path.
// Deploys MockERC20 with 6 decimals (USDC-like) on the simulated chain, checks
// the fresh user starts with a zero balance, then mints 1000 MOCK and confirms
// the command's output reflects the new balance with the correct decimals. The
// non-18 decimals keep the test honest — a hardcoded 10^18 divisor in the
// format path would surface here as the wrong number of whole tokens.
func TestGetPublicBalanceCmd_ERC20_Balance(t *testing.T) {
	testHelper := setupSimTestHelper(t, nil)
	defer testHelper.Close()

	const mockDecimals = 6
	mockAddr := testHelper.DeployMockERC20("Mock Token", "MOCK", mockDecimals)

	key, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	userAddr := ethCommon.HexToAddress(key.PublicKey().Address())

	// Build a TokenRegistry that knows about MOCK. LoadTokenRegistry is the
	// public API — it consumes a properties map, the same format wallet.conf
	// uses, so the test exercises the same code path the CLI uses.
	props := properties.NewProperties()
	_, _, err = props.Set("token.MOCK.address", mockAddr.Hex())
	require.NoError(t, err)
	_, _, err = props.Set("token.MOCK.decimals", "6")
	require.NoError(t, err)
	registry, err := app.LoadTokenRegistry(props)
	require.NoError(t, err)

	runCmd := func() string {
		old := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w
		defer func() { os.Stdout = old }()

		cmd := NewGetPublicBalanceCommand(&app.Config{KeySecp: key, Tokens: registry})
		cmd.Client = testHelper.Client()
		cobraCmd := cmd.Command()
		require.NoError(t, cobraCmd.Flags().Set("token", "MOCK"))
		require.NoError(t, cmd.Exec(t.Context()))

		w.Close()
		var buf bytes.Buffer
		_, err = io.Copy(&buf, r)
		require.NoError(t, err)
		return buf.String()
	}

	// Before mint: zero MOCK. Confirms the ERC-20 path works for a fresh
	// (never-minted-to) address and exercises the FormatAmount zero branch
	// for a non-18-decimal token.
	assert.Contains(t, runCmd(), "0 MOCK")

	// Mint 1000 MOCK (raw = 1000 * 10^6). The decimal count is the point —
	// a hardcoded 10^18 divisor would print "0.000000000001 MOCK" instead.
	mockUnit := new(big.Int).Exp(big.NewInt(10), big.NewInt(mockDecimals), nil)
	mintAmount := new(big.Int).Mul(big.NewInt(1000), mockUnit)
	testHelper.WaitMined(testHelper.MintERC20(userAddr, mintAmount))

	assert.Contains(t, runCmd(), "1000 MOCK")
}
