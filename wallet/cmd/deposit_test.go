package cmd

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes-nova/wallet/cmd/testutil"
	"github.com/horizen-pes/pkg/blockchain"
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepositCmdInvalidDepositAmount(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	depositCmd := NewDepositCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient)
	depositCmd.SubgraphClient = testutil.SubgraphClientOK()
	cmd := depositCmd.Command()

	cmd.Flags().Set("amount", "pippo")
	cmd.Flags().Set("max-value-fee", "100 wei")

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "invalid amount")

}

func TestDepositCmdInvalidMaxValueFee(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	depositCmd := NewDepositCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient)
	depositCmd.SubgraphClient = testutil.SubgraphClientOK()
	cmd := depositCmd.Command()

	cmd.Flags().Set("amount", "333 wei")
	cmd.Flags().Set("max-value-fee", "pippo")

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "invalid max fee amount")

}

func TestDepositCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	depositCmd := NewDepositCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient)
	depositCmd.SubgraphClient = testutil.SubgraphClientOK()
	cmd := depositCmd.Command()

	cmd.Flags().Set("amount", "333 wei")
	cmd.Flags().Set("max-value-fee", "100 wei")

	// To be honest, it should be a StateUpdate but for the test it is enough, for now
	go testutil.CompleteNextRequest(t, testHelper, big.NewInt(65), big.NewInt(35))

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Deposit completed successfully")

}

func TestDepositCmdFailure(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	depositCmd := NewDepositCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient)
	depositCmd.SubgraphClient = testutil.SubgraphClientFailure()
	cmd := depositCmd.Command()

	cmd.Flags().Set("amount", "333 wei")
	cmd.Flags().Set("max-value-fee", "100 wei")

	go testutil.FailNextRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Deposit failed: internal error (code 2)")

}

func TestDepositCmdUsesDefaultMaxValueFee(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)

	depositCmd := NewDepositCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient)
	depositCmd.SubgraphClient = testutil.SubgraphClientOK()
	cmd := depositCmd.Command()

	// Only set amount; max-value-fee should use default value (100 wei)
	cmd.Flags().Set("amount", "333 wei")

	go testutil.CompleteNextRequest(t, testHelper, big.NewInt(65), big.NewInt(35))

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Deposit completed successfully")
}
