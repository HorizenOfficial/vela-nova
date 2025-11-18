package cmd

import (
	"bytes"
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
)

func TestRegisterUserCmd(t *testing.T) {
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
	cmd := NewRegisterUserCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  60,
	}, blockchainClient).Command()

	go testutil.CompleteNextRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Public key registered successfully")

}

func TestRegisterUserCmdFailure(t *testing.T) {
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

	blockchainClient := testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewRegisterUserCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  60,
	}, blockchainClient).Command()

	go testutil.FailNextRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Register user failed: internal error (code 2)")

}

func TestRegisterUserCmdTimeout(t *testing.T) {
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

	blockchainClient := testutil.SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewRegisterUserCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 1,
		BlockchainPollingTimeout:  2,
	}, blockchainClient).Command()

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Timeout expired")

}
