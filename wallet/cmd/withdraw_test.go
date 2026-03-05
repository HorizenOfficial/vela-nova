package cmd

import (
	"bytes"
	"math/big"
	// "math/big"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	pestestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithdrawCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, teeKey.PublicKey().Bytes())
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	withdrawCmd := NewWithdrawCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient)
	withdrawCmd.SubgraphClient = testutil.SubgraphClientOK()
	cmd := withdrawCmd.Command()

	cmd.Flags().Set("amount", "333 wei")
	cmd.Flags().Set("max-value-fee", "100 wei")
	cmd.Flags().Set("to", key1.PublicKey().Address())

	// To be honest, it should be a StateUpdate but the test it is enough for now
	go testutil.CompleteNextRequest(t, testHelper, big.NewInt(50), big.NewInt(50))

	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Withdrawal")

}

func TestWithdrawCmdFailure(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, teeKey.PublicKey().Bytes())
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	withdrawCmd := NewWithdrawCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient)
	withdrawCmd.SubgraphClient = testutil.SubgraphClientFailure()
	cmd := withdrawCmd.Command()

	cmd.Flags().Set("amount", "333 wei")
	cmd.Flags().Set("max-value-fee", "100 wei")
	cmd.Flags().Set("to", key1.PublicKey().Address())

	go testutil.FailNextRequest(t, testHelper)

	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Withdrawal failed: internal error (code 2)")

}
