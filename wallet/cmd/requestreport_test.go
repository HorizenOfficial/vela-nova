package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes-nova/wallet/cmd/testutil"
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)



func TestRequestReportCmd(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, teeKey.PublicKey().Bytes())
	defer testHelper.Close()

 
	Key1 := &cryptotypes.PrivateKeySecp256k1{PrivateKey: testHelper.ManagerPrivKey}
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	// Set the authority to be the testHelper manager account
	tx := testHelper.AddAuthority(&NOVA_APPLICATION_ID, testHelper.ManagerAccount.From)
	testHelper.WaitMined(tx)

	t.Run("Command successful", func(t *testing.T) { 
		// Redirect stdout
		old := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w


		// Execute the command
		blockchainClient := testutil.SetupNewBlockChainClient(testHelper)
		cmd := NewRequestReportCommand(&app.Config{
			KeySecp:                   Key1, 
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
		assert.Contains(t, output, "Deanonymization request completed successfully")

	})


	t.Run("Command failed", func(t *testing.T) { 
		// Redirect stdout
		old := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		// Execute the command
		blockchainClient := testutil.SetupNewBlockChainClient(testHelper)
		cmd := NewRequestReportCommand(&app.Config{
			KeySecp:                   Key1, 
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
		assert.Contains(t, output, "Request report failed: internal error (code 2)")

	})


}

