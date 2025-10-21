package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"fmt"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockBCClient struct {
	blockchain.BlockChainClient
	f func(context.Context) ([]*common.Request, error) 
}

func (m * MockBCClient) GetPendingRequests(ctx context.Context) ([]*common.Request, error) {
	if m.f != nil {
		return m.f(ctx)
	}
	return nil, nil
}


func TestRegisterUserCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	testHelper := testutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	blockchainClient := SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewRegisterUserCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout: 60,
	}, blockchainClient).Command()

	go completeKeyRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Public key registered successfully")

}


func TestRegisterUserCmdFailure(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	testHelper := testutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	blockchainClient := SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewRegisterUserCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout: 60,
	}, blockchainClient).Command()

	go failKeyRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Public key registration failed")

}

func TestRegisterUserCmdTimeout(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	testHelper := testutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	blockchainClient := SetupNewBlockChainClient(testHelper)
	// Execute the command
	cmd := NewRegisterUserCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		BlockchainPollingInterval: 20,
		BlockchainPollingTimeout: 1,
	}, blockchainClient).Command()

	go failKeyRequest(t, testHelper)

	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	fmt.Println(output)
	assert.Contains(t, output, "Timeout expired")

}



func SetupNewBlockChainClient(testHelper *testutil.SimTestHelper) *blockchain.BlockChainClient {
	return blockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress,  testHelper.TeeSignerAddress, testHelper.ManagerAccount)

}

func completeKeyRequest(t *testing.T, testHelper *testutil.SimTestHelper) {
	blockchainClient := SetupNewBlockChainClient(testHelper)

	for {
		request, _, err := blockchainClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			err = blockchainClient.MarkRequestCompleted(context.Background(), request.RequestID)
			require.NoError(t, err)
			return
		}

	}
}

func failKeyRequest(t *testing.T, testHelper *testutil.SimTestHelper) {
	blockchainClient := SetupNewBlockChainClient(testHelper)

	for {
		request, _, err := blockchainClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			err = blockchainClient.MarkRequestFailed(context.Background(), request.RequestID)
			require.NoError(t, err)
			return
		}

	}
}