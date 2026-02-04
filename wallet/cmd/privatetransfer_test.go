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
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
)

func TestPrivateTransfer(t *testing.T) {

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	var teeKey, _ = crypto.GeneratePrivateKeyP521()

	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, teeKey.PublicKey().Bytes())
	defer testHelper.Close()

	client := testutil.SetupNewBlockChainClient(testHelper)

	// Execute the command
	transferCmd := NewPrivateTransferCommand(&app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, client)
	transferCmd.SubgraphClient = testutil.SubgraphClientOK()
	cmd := transferCmd.Command()
	cmd.Flags().Set("amount", "1 ETH")
	cmd.Flags().Set("to", "0x0000000000000000000000000000000000000001")
	cmd.Flags().Set("max-value-fee", "100 wei")

	go testutil.CompleteNextRequest(t, testHelper, big.NewInt(50), big.NewInt(50))

	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Private transfer completed successfully")

}
