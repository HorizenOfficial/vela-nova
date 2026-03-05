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

func TestClaimPendingPaymentsCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)

	claimPendingCmd := NewClaimPendingPaymentsCommand(&app.Config{
		KeySecp: key1,
	}, blockchainClient)
	cmd := claimPendingCmd.Command()

	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "Nothing to claim")
}

func TestClaimPendingPaymentsCmdNoKey(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	var blockchainClient blockchain.Client = testutil.SetupNewBlockChainClient(testHelper)

	claimPendingCmd := NewClaimPendingPaymentsCommand(&app.Config{}, blockchainClient)
	cmd := claimPendingCmd.Command()

	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "Secp256k1 key not found")
}
