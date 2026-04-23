package cmd

import (
	"bytes"
	"io"
	"math/big"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	pestestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	ethCommon "github.com/ethereum/go-ethereum/common"
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

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
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

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
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
