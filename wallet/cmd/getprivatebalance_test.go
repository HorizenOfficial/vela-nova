package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/horizen-pes/pkg/subgraph"
	"github.com/stretchr/testify/assert"
)

// Test blockchain client that returns a fixed TEE public key.
type TestGetPrivateBalanceBlockChainClient struct {
	*blockchain.MockClient
	teePub *cryptotypes.PublicKeyP521
}

func (c *TestGetPrivateBalanceBlockChainClient) GetTeePublicKey(ctx context.Context) (*cryptotypes.PublicKeyP521, error) {
	return c.teePub, nil
}

func TestGetPrivateBalance(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	//prepare args
	mockEvent := []byte(`{"` + BALANCE_JSON_KEY + `": 12345}`)
	encrypted, _ := crypto.Encrypt(teeKey, key2.PublicKey(), mockEvent)

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	sgClient := subgraph.NewMockClient().WithUserEvents(NOVA_APPLICATION_ID, []subgraph.UserEvent{
		{
			ApplicationID: NOVA_APPLICATION_ID,
			EncryptedData: encrypted,
		},
	})
	// Execute the command
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(&app.Config{
		KeySecp: key1,
		KeyP521: key2,
		RpcUrl:  "https://base-sepolia.drpc.org",
	}, client)
	getPrivateBalanceCmd.SubgraphClient = sgClient
	cmd := getPrivateBalanceCmd.Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "0.000000000000012345")
}

func TestGetPrivateBalance_BalanceZero(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	//prepare args
	mockEvent := []byte("NOT A VALID JSON EVENT")
	encrypted, _ := crypto.Encrypt(teeKey, key2.PublicKey(), mockEvent)

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	sgClient := subgraph.NewMockClient().WithUserEvents(NOVA_APPLICATION_ID, []subgraph.UserEvent{
		{
			ApplicationID: NOVA_APPLICATION_ID,
			EncryptedData: encrypted,
		},
	})
	// Execute the command
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(&app.Config{
		KeySecp: key1,
		KeyP521: key2,
		RpcUrl:  "https://base-sepolia.drpc.org",
	}, client)
	getPrivateBalanceCmd.SubgraphClient = sgClient
	cmd := getPrivateBalanceCmd.Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "0.000000000000000000")
}
