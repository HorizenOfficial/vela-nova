package cmd

import (
	"bytes"
	"io"
	"math/big"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	pestestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/stretchr/testify/assert"
)

func TestPrivateTransfer(t *testing.T) {

	// helper: run a private transfer command with optional invoice ID, return captured stdout
	doTransfer := func(t *testing.T, invoiceID string) string {
		t.Helper()

		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		var key1, _ = crypto.GeneratePrivateKeySecp256k1()
		var key2, _ = crypto.GeneratePrivateKeyP521()
		var teeKey, _ = crypto.GeneratePrivateKeyP521()

		testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, teeKey.PublicKey().Bytes())
		defer testHelper.Close()

		appID := testutil.DeployTestApplication(t, testHelper)

		client := testutil.SetupNewBlockChainClient(testHelper)

		cfg := &app.Config{
			KeySecp:                   key1,
			KeyP521:                   key2,
			ApplicationID:             appID,
			BlockchainPollingInterval: 2,
			BlockchainPollingTimeout:  10,
		}
		testutil.WriteTempConf(t, cfg)
		transferCmd := NewPrivateTransferCommand(cfg, client)
		transferCmd.SubgraphClient = testutil.SubgraphClientOK()
		cmd := transferCmd.Command()
		cmd.Flags().Set("amount", "1 ETH")
		cmd.Flags().Set("to", "0x0000000000000000000000000000000000000001")
		cmd.Flags().Set("max-value-fee", "100 wei")
		if invoiceID != "" {
			cmd.Flags().Set("invoice-id", invoiceID)
		}

		go testutil.CompleteNextRequest(t, testHelper, big.NewInt(50), big.NewInt(50))

		cmd.Run(cmd, nil)

		w.Close()
		os.Stdout = old

		var buf bytes.Buffer
		io.Copy(&buf, r)
		return buf.String()
	}

	t.Run("WithoutInvoiceID", func(t *testing.T) {
		output := doTransfer(t, "")
		assert.Contains(t, output, "Private transfer completed successfully")
	})

	t.Run("WithInvoiceID", func(t *testing.T) {
		output := doTransfer(t, "INV-2025-001")
		assert.Contains(t, output, "Private transfer completed successfully")
	})
}
