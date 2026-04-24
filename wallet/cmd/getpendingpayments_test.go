package cmd

import (
	"bytes"
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

func TestGetPendingPaymentsCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	autoMining, useMockContracts := true, true
	testHelper := pestestutil.NewSimTestHelper(t, autoMining, useMockContracts, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)

	cfg := &app.Config{KeySecp: key1}
	testutil.WriteTempConf(t, cfg)
	getPendingCmd := NewGetPendingPaymentsCommand(cfg, blockchainClient)
	cmd := getPendingCmd.Command()

	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "Pending claims: 0 ETH")
}

func TestGetPendingPaymentsCmdNoKey(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	autoMining, useMockContracts := true, true
	testHelper := pestestutil.NewSimTestHelper(t, autoMining, useMockContracts, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)

	cfg := &app.Config{}
	testutil.WriteTempConf(t, cfg)
	getPendingCmd := NewGetPendingPaymentsCommand(cfg, blockchainClient)
	cmd := getPendingCmd.Command()

	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "Secp256k1 key not found")
}
