package testutil

import (
	"context"
	"math/big"
	"testing"

	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/common/apperrors"
	"github.com/horizen-pes/pkg/subgraph"
	"github.com/stretchr/testify/require"
)

func SetupNewBlockChainClient(testHelper *testutil.SimTestHelper) *blockchain.BlockChainClient {
	return blockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress, testHelper.TeeSignerAddress, testHelper.ManagerAccount)

}

func CompleteNextRequest(t *testing.T, testHelper *testutil.SimTestHelper, refundAmount *big.Int, applicationFees *big.Int) {
	blockchainClient := SetupNewBlockChainClient(testHelper)

	for {
		request, _, err := blockchainClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			err = blockchainClient.MarkRequestCompleted(context.Background(), request.RequestID, refundAmount, applicationFees)
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
			err = blockchainClient.MarkRequestFailed(context.Background(), request.RequestID, apperrors.New(apperrors.CodeInternalFallback, "internal error", nil))
			require.NoError(t, err)
			return
		}

	}
}

// StubSubgraphClient returns canned RequestCompleted responses for tests.
type StubSubgraphClient struct {
	Result *subgraph.RequestCompleted
	Err    error
}

func (s StubSubgraphClient) GetRequestCompletedByID(_ context.Context, _ common.RequestIdType) (*subgraph.RequestCompleted, error) {
	return s.Result, s.Err
}

func (StubSubgraphClient) GetUserEvents(context.Context, common.ApplicationIdType, string, int) ([]subgraph.UserEvent, error) {
	return nil, nil
}

func SubgraphClientOK() subgraph.Client {
	return StubSubgraphClient{Result: &subgraph.RequestCompleted{Status: common.RequestResultOK}}
}

func SubgraphClientFailure() subgraph.Client {
	return StubSubgraphClient{Result: &subgraph.RequestCompleted{
		Status:       common.RequestResultFailed,
		ErrorCode:    2,
		ErrorMessage: "internal error",
	}}
}

func SubgraphClientEmpty() subgraph.Client {
	return StubSubgraphClient{}
}
