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
	"github.com/horizen-pes/pkg/common"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
)

// create a test blockchain client with only the GetUserEvents method defined
type TestGetPrivateBalanceBlockChainClient struct {
	eventToReturn []byte
	blockToReturn uint64
	lastToBlock uint64
}

//methods to implement interface
func (c TestGetPrivateBalanceBlockChainClient) SubmitRequest(ctx context.Context, protocolVersion uint8, applicationId *big.Int, requestType common.RequestType, payload []byte, value *big.Int) (string, uint64, error) {
	return "", 0, nil
}
func (c TestGetPrivateBalanceBlockChainClient) GetPendingRequests(ctx context.Context) ([]*common.Request, error) {
	return nil, nil
}
func (c TestGetPrivateBalanceBlockChainClient) GetNextPendingRequest(ctx context.Context) (*common.Request, [32]byte, error) {
	return nil, [32]byte{}, nil
}
func (c TestGetPrivateBalanceBlockChainClient) MarkRequestFailed(ctx context.Context, requestID string) error {
	return nil
}
func (c TestGetPrivateBalanceBlockChainClient) SubmitStateUpdate(ctx context.Context, update *common.UpdatePayload) error {
	return nil
}
func (c TestGetPrivateBalanceBlockChainClient) SubmitDeanonymizationReport(ctx context.Context, update *common.DeanonymizationReport) error {
	return nil
}
func (c TestGetPrivateBalanceBlockChainClient) Close() error {
	return nil
}
func (c TestGetPrivateBalanceBlockChainClient) Connect(ctx context.Context) error {
	return nil
}
///rewrite GetUserEvents
func (c TestGetPrivateBalanceBlockChainClient) GetUserEvents(ctx context.Context, privKey cryptotypes.PrivateKeyP521, applicationId big.Int, fromBlock uint64, toBlock uint64, filter func([]byte) bool, stopAtFirst bool) ([][]byte, error) {
	if fromBlock < toBlock {
		return [][]byte{}, fmt.Errorf("fromBlock should be greater than toBlock: %d, %d", fromBlock, toBlock)
	}
	//check that the blocks are searched with continuity
	if c.lastToBlock != 0 && fromBlock != c.lastToBlock - 1 {
		return [][]byte{}, fmt.Errorf("when searching again, no block should be skipped: %d, %d", fromBlock, c.lastToBlock)
	}
	c.lastToBlock = toBlock
	//mocked function: the event is at the given block
	if fromBlock >= c.blockToReturn && toBlock <= c.blockToReturn {
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
	balanceStr := "12345"
	args := []string{"1"}
	mockEvent := []byte(`{"balance": "` + balanceStr + `"}`)

	client := TestGetPrivateBalanceBlockChainClient{
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
	cmd.Run(nil, args)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, balanceStr)
}