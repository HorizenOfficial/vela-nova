package cmd

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	pestestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestReportCmd(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, teeKey.PublicKey().Bytes())
	defer testHelper.Close()

	appID := testutil.DeployApplication(t, testHelper)
	origAppID := NOVA_APPLICATION_ID
	NOVA_APPLICATION_ID = appID
	defer func() { NOVA_APPLICATION_ID = origAppID }()

	Key1 := &cryptotypes.PrivateKeySecp256k1{PrivateKey: testHelper.ManagerPrivKey}
	key2, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	// Set the authority to be the testHelper manager account
	tx := testHelper.AddAuthority(new(big.Int).SetUint64(uint64(appID)), testHelper.ManagerAccount.From)
	testHelper.WaitMined(tx)

	t.Run("Command successful", func(t *testing.T) {
		// Redirect stdout
		old := os.Stdout
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = w

		// Execute the command
		blockchainClient := testutil.SetupNewBlockChainClient(testHelper)
		reportCmd := NewRequestReportCommand(&app.Config{
			KeySecp:                   Key1,
			KeyP521:                   key2,
			BlockchainPollingInterval: 2,
			BlockchainPollingTimeout:  60,
		}, blockchainClient)
		reportCmd.SubgraphClient = testutil.SubgraphClientOK()
		cmd := reportCmd.Command()
		cmd.Flags().Set("max-value-fee", "100 wei")

		go testutil.CompleteNextRequest(t, testHelper, big.NewInt(80), big.NewInt(20))
		cmd.Run(cmd, nil)

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
		reportCmd := NewRequestReportCommand(&app.Config{
			KeySecp:                   Key1,
			KeyP521:                   key2,
			BlockchainPollingInterval: 2,
			BlockchainPollingTimeout:  60,
		}, blockchainClient)
		reportCmd.SubgraphClient = testutil.SubgraphClientFailure()
		cmd := reportCmd.Command()
		cmd.Flags().Set("max-value-fee", "100 wei")

		go testutil.FailNextRequest(t, testHelper)
		cmd.Run(cmd, nil)

		// Restore stdout
		w.Close()
		os.Stdout = old

		var buf bytes.Buffer
		io.Copy(&buf, r)
		output := buf.String()

		fmt.Println(output)
		assert.Contains(t, output, "Deanonymization request failed: internal error (code 2)")

	})

}
