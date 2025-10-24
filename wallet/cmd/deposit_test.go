package cmd

import (
	"bytes"
	"math/big"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes-nova/wallet/cmd/testutil"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepositCmdInvalidInput(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewDepositCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout: 10,
	}, blockchainClient).Command()

	cmd.Flags().Set("amount", "pippo")


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

func TestDepositCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewDepositCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout: 10,
	}, blockchainClient).Command()

	cmd.Flags().Set("amount", "333 wei")

	// To be honest, it should be a StateUpdate but the test it is enough for now
	go testutil.CompleteKeyRequest(t, testHelper)

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
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewDepositCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout: 10,
	}, blockchainClient).Command()

	cmd.Flags().Set("amount", "333 wei")


	go testutil.FailKeyRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Deposit failed")

}


// // func TestRegisterUserCmdTimeout(t *testing.T) {
// // 	// Redirect stdout
// // 	old := os.Stdout
// // 	r, w, _ := os.Pipe()
// // 	os.Stdout = w

// // 	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
// // 	var key2, _ = crypto.GeneratePrivateKeyP521()

// // 	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
// // 	defer testHelper.Close()

// // 	blockchainClient := SetupNewBlockChainClient(testHelper)
// // 	// Execute the command
// // 	cmd := NewRegisterUserCommand(&app.Config{
// // 		KeySecp: *key1,
// // 		KeyP521: *key2,
// // 		BlockchainPollingInterval: 20,
// // 		BlockchainPollingTimeout: 1,
// // 	}, blockchainClient).Command()

// // 	go failKeyRequest(t, testHelper)

// // 	cmd.Run(nil, nil)

// // 	// Restore stdout
// // 	w.Close()
// // 	os.Stdout = old

// // 	var buf bytes.Buffer
// // 	io.Copy(&buf, r)
// // 	output := buf.String()

// // 	fmt.Println(output)
// // 	assert.Contains(t, output, "Timeout expired")

// // }


func TestParseEtherValue(t *testing.T) {


	input := ""
	_, err := ParseEtherValue(input)
	require.Error(t, err)

	input = "34445 wei"
	value, err := ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(34445), value)

	input = "34445 Wei"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(34445), value)

	input = "34445 WEI"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(34445), value)

	input = "34445"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "1.5 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(15e17), value)

	input = "1.5 Wei"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "100 Gwei"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100e9), value)

	input = "100.0 GWEI"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100e9), value)

	input = "100.0 gwei"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100e9), value)

	input = "0.000000001 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1e9), value)

	input = "  5.0 GWEI  "
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(5e9), value)

	input = "invalid input"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "100 USD"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

}