package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes-nova/wallet/cmd/testutil"
	"github.com/horizen-pes/pkg/blockchain"
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
)

type TestPrivateTransferBlockChainClient struct {
	blockchain.Client
}
func (c *TestPrivateTransferBlockChainClient) GetTeePublicKey(ctx context.Context) (*cryptotypes.PublicKeyP521, error) {
	key, err := crypto.GeneratePrivateKeyP521()
	return key.PublicKey(), err
}

func TestPrivateTransfer(t *testing.T) {

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	
	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()
	client := &TestPrivateTransferBlockChainClient{
		testutil.SetupNewBlockChainClient(testHelper),
	}
	// Execute the command
	cmd := NewPrivateTransferCommand(&app.Config{
		KeySecp: key1,
		KeyP521: key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, client).Command()
	cmd.Flags().Set("amount", "1 ETH")
	cmd.Flags().Set("to", "0x0000000000000000000000000000000000000001")
	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	fmt.Println(output)
	assert.Contains(t, output, "Private transfer completed successfully")

}
