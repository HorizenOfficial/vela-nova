package testutil

import (
	"context"
	"crypto/rand"
	"math/big"
	"testing"

	cceblockchain "github.com/horizen-cce-common-go/wallet/blockchain"
	ccecommon "github.com/horizen-cce-common-go/wallet/common"
	"github.com/horizen-cce-common-go/wallet/subgraph"
	pesblockchain "github.com/horizen-pes/pkg/blockchain"
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/common/apperrors"
	"github.com/stretchr/testify/require"
)

func SetupNewBlockChainClient(testHelper *pestestutil.SimTestHelper) *cceblockchain.BlockChainClient {
	return cceblockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress, testHelper.TeeSignerAddress, testHelper.ManagerAccount)
}

func CompleteNextRequest(t *testing.T, testHelper *pestestutil.SimTestHelper, refundAmount *big.Int, applicationFees *big.Int) {
	coreClient := pesblockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress, testHelper.TeeSignerAddress, testHelper.ManagerAccount)

	for {
		request, stateRoot, err := coreClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			var newStateRoot [32]byte
			_, err = rand.Read(newStateRoot[:]) // dummy new state root
			require.NoError(t, err)
			update := &common.UpdatePayload{
				ApplicationID:  request.ApplicationID,
				RequestID:      request.RequestID,
				PrevStateRoot:  stateRoot,
				NewStateRoot:   newStateRoot,
				Signature:      make([]byte, 65),
				RefundAmount:   common.ToBig(refundAmount),
				ApplicationFee: common.ToBig(applicationFees),
			}
			err = coreClient.SubmitStateUpdate(context.Background(), update)
			require.NoError(t, err)
			return
		}
	}
}

func FailNextRequest(t *testing.T, testHelper *pestestutil.SimTestHelper) {
	coreClient := pesblockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress, testHelper.TeeSignerAddress, testHelper.ManagerAccount)

	for {
		request, _, err := coreClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			err = coreClient.MarkRequestFailed(context.Background(), request.RequestID, apperrors.New(apperrors.CodeInternalFallback, "internal error", nil))
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

func (s StubSubgraphClient) GetRequestCompletedByID(_ context.Context, _ ccecommon.RequestIdType) (*subgraph.RequestCompleted, error) {
	return s.Result, s.Err
}

func (StubSubgraphClient) HealthCheck(context.Context) error {
	return nil
}

func (StubSubgraphClient) GetUserEvents(context.Context, ccecommon.ApplicationIdType, string, int, *big.Int) ([]subgraph.UserEvent, error) {
	return nil, nil
}

func SubgraphClientOK() subgraph.Client {
	return StubSubgraphClient{Result: &subgraph.RequestCompleted{Status: ccecommon.RequestResultOK}}
}

func SubgraphClientFailure() subgraph.Client {
	return StubSubgraphClient{Result: &subgraph.RequestCompleted{
		Status:       ccecommon.RequestResultFailed,
		ErrorCode:    2,
		ErrorMessage: "internal error",
	}}
}

func SubgraphClientEmpty() subgraph.Client {
	return StubSubgraphClient{}
}
