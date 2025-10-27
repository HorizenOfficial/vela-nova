package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
)

// create a test blockchain client with only the GetUserEvents method defined
type TestGetPrivateBalanceBlockChainClient struct {
	blockchain.MockClient
	eventToReturn []byte
	blockToReturn uint64
	lastToBlock uint64
}

///rewrite GetUserEvents
func (c *TestGetPrivateBalanceBlockChainClient) GetUserEvents(ctx context.Context, privKey cryptotypes.PrivateKeyP521, applicationId big.Int, fromBlock uint64, toBlock uint64, filter func([]byte) bool, stopAtFirst bool) ([][]byte, error) {
	if fromBlock < toBlock {
		return [][]byte{}, fmt.Errorf("fromBlock should be greater than toBlock: %d, %d", fromBlock, toBlock)
	}
	//check that the blocks are searched with continuity
	if c.lastToBlock != 0 && fromBlock != c.lastToBlock - 1 {
		return [][]byte{}, fmt.Errorf("when searching again, no block should be skipped: %d, %d", fromBlock, c.lastToBlock)
	}
	c.lastToBlock = toBlock
	//mocked function: the event is at the given block
	if fromBlock >= c.blockToReturn && toBlock <= c.blockToReturn && filter(c.eventToReturn) {
		return [][]byte{c.eventToReturn}, nil
	}
	return [][]byte{}, nil
}

func TestGetPrivateBalance(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	//prepare args
	mockEvent := []byte(`{"` + BALANCE_JSON_KEY + `": "12345"}`)

	client := &TestGetPrivateBalanceBlockChainClient{
		*blockchain.NewMockClient(),
		mockEvent,
		3, //the event is returned when searching in the block 3
		0,
	}
	// Execute the command
	cmd := NewGetPrivateBalanceCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		RpcUrl: "https://base-sepolia.drpc.org",
	}, client).Command()
	cmd.Run(nil, nil)

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

	//prepare args
	mockEvent := []byte("NOT A VALID JSON EVENT")

	client := &TestGetPrivateBalanceBlockChainClient{
		*blockchain.NewMockClient(),
		mockEvent, //since the event is not valid, it will be filtered out and we'll arrive at the end without events, returning 0
		3, //the event is returned when searching in the block 3
		0,
	}
	// Execute the command
	cmd := NewGetPrivateBalanceCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		RpcUrl: "https://base-sepolia.drpc.org",
	}, client).Command()
	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, "0.000000000000000000")
}