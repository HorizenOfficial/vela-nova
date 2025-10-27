package cmd

import (
	"bytes"
	// "math/big"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes-nova/wallet/cmd/testutil"
	"github.com/horizen-pes/pkg/blockchain"
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	// "github.com/stretchr/testify/require"
)

// func TestDepositCmdInvalidInput(t *testing.T) {
// 	// Redirect stdout
// 	old := os.Stdout
// 	r, w, _ := os.Pipe()
// 	os.Stdout = w

// 	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
// 	var key2, _ = crypto.GeneratePrivateKeyP521()

// 	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
// 	defer testHelper.Close()

// 	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
// 	// Execute the command
// 	cmd := NewDepositCommand(&app.Config{
// 		KeySecp: *key1,
// 		KeyP521: *key2,
// 		BlockchainPollingInterval: 2,
// 		BlockchainPollingTimeout: 10,
// 	}, blockchainClient).Command()

// 	cmd.Flags().Set("amount", "pippo")

// 	cmd.Run(nil, nil)

// 	// Restore stdout
// 	w.Close()
// 	os.Stdout = old

// 	var buf bytes.Buffer
// 	io.Copy(&buf, r)
// 	output := buf.String()

// 	fmt.Println(output)
// 	assert.Contains(t, output, "invalid amount")

// }

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
	cmd := NewWithdrawCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, blockchainClient).Command()

	cmd.Flags().Set("amount", "333 wei")
	cmd.Flags().Set("to", key1.PublicKey().Address())

	// To be honest, it should be a StateUpdate but the test it is enough for now
	go testutil.CompletePendingRequest(t, testHelper)

	cmd.Run(nil, nil)

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
	cmd := NewWithdrawCommand(&app.Config{
		KeySecp: key1,
		KeyP521: key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout: 10,
	}, blockchainClient).Command()

	cmd.Flags().Set("amount", "333 wei")
	cmd.Flags().Set("to", key1.PublicKey().Address())

	go testutil.FailPendingRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Withdrawal failed")

}
