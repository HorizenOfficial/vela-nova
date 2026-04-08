package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/HorizenOfficial/vela-common-go/subgraph"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/HorizenOfficial/vela/pkg/common/apperrors"
	"github.com/stretchr/testify/require"
)

func SetupNewBlockChainClient(testHelper *testutil.SimTestHelper) *blockchain.BlockChainClient {
	return blockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress, testHelper.TeeSignerAddress, testHelper.ManagerAccount)
}

// DeployApplication submits a deploy request using the Deployer account (which holds the
// DEPLOYER_ROLE) and synchronously completes it so the application is registered on-chain
// before the test's command runs. Returns the dynamically assigned application ID.
func DeployApplication(t *testing.T, testHelper *testutil.SimTestHelper) common.ApplicationIdType {
	t.Helper()

	testHelper.SubmitDeployRequest(nil, big.NewInt(100))

	blockchainClient := SetupNewBlockChainClient(testHelper)
	deadline := time.Now().Add(15 * time.Second)

	for {
		if time.Now().After(deadline) {
			panic(fmt.Sprintf("timeout waiting for deploy request in %s", t.Name()))
		}

		request, stateRoot, err := blockchainClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			var newStateRoot [32]byte
			_, err = rand.Read(newStateRoot[:])
			require.NoError(t, err)
			update := &common.UpdatePayload{
				ApplicationID:  request.ApplicationID,
				RequestID:      request.RequestID,
				PrevStateRoot:  stateRoot,
				NewStateRoot:   newStateRoot,
				Signature:      make([]byte, 65),
				RefundAmount:   common.NewBig(0),
				ApplicationFee: common.NewBig(100), // must equal the maxFeeValue passed to SubmitDeployRequest
			}
			err = blockchainClient.SubmitStateUpdate(context.Background(), update)
			require.NoError(t, err)
			return request.ApplicationID
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func CompleteNextRequest(t *testing.T, testHelper *testutil.SimTestHelper, refundAmount *big.Int, applicationFees *big.Int) {
	t.Helper()

	blockchainClient := SetupNewBlockChainClient(testHelper)
	deadline := time.Now().Add(15 * time.Second)

	for {
		if time.Now().After(deadline) {
			panic(fmt.Sprintf("timeout waiting for next pending request to complete in %s", t.Name()))
		}

		request, stateRoot, err := blockchainClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			var newStateRoot [32]byte
			_, err = rand.Read(newStateRoot[:]) //dummy new state root
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
			err = blockchainClient.SubmitStateUpdate(context.Background(), update)
			require.NoError(t, err)
			return
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func FailNextRequest(t *testing.T, testHelper *testutil.SimTestHelper) {
	t.Helper()

	blockchainClient := SetupNewBlockChainClient(testHelper)
	deadline := time.Now().Add(15 * time.Second)

	for {
		if time.Now().After(deadline) {
			panic(fmt.Sprintf("timeout waiting for next pending request to fail in %s", t.Name()))
		}

		request, stateRoot, err := blockchainClient.GetNextPendingRequest(context.Background())
		require.NoError(t, err)
		if request != nil {
			update := &common.UpdatePayload{
				ApplicationID:  request.ApplicationID,
				RequestID:      request.RequestID,
				PrevStateRoot:  stateRoot,
				NewStateRoot:   stateRoot, // same as prev state root for failed requests
				Signature:      make([]byte, 65),
				RefundAmount:   common.ToBig(request.DepositAmount.ToInt()),
				ApplicationFee: common.NewBig(0),
				ErrorCode:      apperrors.New(apperrors.CodeInternalFallback, "internal error").Category(),
				ErrorMsg:       "internal error",
			}
			err = blockchainClient.SubmitStateUpdate(context.Background(), update)
			require.NoError(t, err)
			return
		}

		time.Sleep(50 * time.Millisecond)
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

func (StubSubgraphClient) HealthCheck(context.Context) error {
	return nil
}

func (StubSubgraphClient) GetUserEvents(context.Context, common.ApplicationIdType, string, int, *big.Int) ([]subgraph.UserEvent, error) {
	return nil, nil
}

func (StubSubgraphClient) GetUserEventsBySubTypes(context.Context, common.ApplicationIdType, []string, int, *big.Int) ([]subgraph.UserEvent, error) {
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
