package testutil

import (
	"context"
	"testing"

	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/stretchr/testify/require"
)

func SetupNewBlockChainClient(testHelper *testutil.SimTestHelper) *blockchain.BlockChainClient {
	return blockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress, testHelper.TeeSignerAddress, testHelper.ManagerAccount)

}

func CompleteNextRequest(t *testing.T, testHelper *testutil.SimTestHelper) {
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

func FailNextRequest(t *testing.T, testHelper *testutil.SimTestHelper) {
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
