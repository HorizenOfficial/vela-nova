package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/HorizenOfficial/vela-common-go/subgraph"
	"github.com/stretchr/testify/assert"
)

var testAppID = common.NewApplicationId(1)

// Test blockchain client that returns a fixed TEE public key.
type TestGetPrivateBalanceBlockChainClient struct {
	*blockchain.MockClient
	teePub *cryptotypes.PublicKeyP521
}

func (c *TestGetPrivateBalanceBlockChainClient) GetTeePublicKey(ctx context.Context) (*cryptotypes.PublicKeyP521, error) {
	return c.teePub, nil
}

func TestGetPrivateBalance_HexEncodedBalance(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	// Balance must be hex-encoded with 0x prefix for common.Big unmarshaling
	// 12345 decimal = 0x3039 hex
	mockEvent := []byte(`{"balance": "0x3039"}`)
	encrypted, _ := crypto.Encrypt(teeKey, key2.PublicKey(), mockEvent)

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	sgClient := subgraph.NewMockClient().WithUserEvents(testAppID, []subgraph.UserEvent{
		{
			ApplicationID: testAppID,
			EncryptedData: encrypted,
		},
	})
	// Execute the command
	cfg := &app.Config{
		KeySecp:       key1,
		KeyP521:       key2,
		RpcUrl:        "https://base-sepolia.drpc.org",
		ApplicationID: testAppID,
	}
	testutil.WriteTempConf(t, cfg)
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(cfg, client)
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

func TestGetPrivateBalance_NoEventsReturnsZero(t *testing.T) {
	// This test verifies that when no events are found, the default zero balance
	// is returned and properly unmarshaled. 
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	// Empty events - no user events returned from subgraph
	sgClient := subgraph.NewMockClient().WithUserEvents(testAppID, []subgraph.UserEvent{})

	cfg := &app.Config{
		KeySecp:       key1,
		KeyP521:       key2,
		RpcUrl:        "https://base-sepolia.drpc.org",
		ApplicationID: testAppID,
	}
	testutil.WriteTempConf(t, cfg)
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(cfg, client)
	getPrivateBalanceCmd.SubgraphClient = sgClient
	cmd := getPrivateBalanceCmd.Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "0 ETH")
}

func TestGetPrivateBalance_InvalidEventFilteredOut(t *testing.T) {
	// When an event doesn't pass the filter (no Balance key), we fall back to zero
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	// Event without Balance key - will be filtered out
	mockEvent := []byte(`{"other_field": "value"}`)
	encrypted, _ := crypto.Encrypt(teeKey, key2.PublicKey(), mockEvent)

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	sgClient := subgraph.NewMockClient().WithUserEvents(testAppID, []subgraph.UserEvent{
		{
			ApplicationID: testAppID,
			EncryptedData: encrypted,
		},
	})

	cfg := &app.Config{
		KeySecp:       key1,
		KeyP521:       key2,
		RpcUrl:        "https://base-sepolia.drpc.org",
		ApplicationID: testAppID,
	}
	testutil.WriteTempConf(t, cfg)
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(cfg, client)
	getPrivateBalanceCmd.SubgraphClient = sgClient
	cmd := getPrivateBalanceCmd.Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "0 ETH")
}
