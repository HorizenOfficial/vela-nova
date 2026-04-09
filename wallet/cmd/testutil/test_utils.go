package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/HorizenOfficial/vela-common-go/subgraph"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/HorizenOfficial/vela/pkg/common/apperrors"
	"github.com/stretchr/testify/require"
)

func SetupNewBlockChainClient(testHelper *testutil.SimTestHelper) *blockchain.BlockChainClient {
	return blockchain.SetupNewBlockChainClientConnected(testHelper.Client(), testHelper.ProcessorContractAddress, testHelper.TeeSignerAddress, testHelper.ManagerAccount)

}

// DeployTestApplication deploys and registers an application on the simulated
// blockchain, returning the system-assigned application ID.
func DeployTestApplication(t *testing.T, testHelper *testutil.SimTestHelper) common.ApplicationIdType {
	t.Helper()

	blockchainClient := SetupNewBlockChainClient(testHelper)

	// Submit deploy request (uses the Deployer account)
	deployTx := testHelper.SubmitDeployRequest(nil, big.NewInt(100))
	testHelper.WaitMined(deployTx)

	// Get the pending deploy request to extract the assigned applicationId
	deployReq, deployStateRoot, err := blockchainClient.GetNextPendingRequest(context.Background())
	require.NoError(t, err)
	require.NotNil(t, deployReq, "expected a pending deploy request")

	// Complete the deploy with a successful stateUpdate
	err = blockchainClient.SubmitStateUpdate(context.Background(), &common.UpdatePayload{
		ApplicationID:  deployReq.ApplicationID,
		RequestID:      deployReq.RequestID,
		PrevStateRoot:  deployStateRoot,
		NewStateRoot:   [32]byte{0x01, 0x02, 0x03},
		Events:         []common.Event{},
		Withdrawals:    []common.Withdrawal{},
		Signature:      make([]byte, 65),
		RefundAmount:   common.NewBig(95),
		ApplicationFee: common.NewBig(5),
	})
	require.NoError(t, err)

	return deployReq.ApplicationID
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
				RefundAmount:   common.ToBig(request.AssetAmount.ToInt()),
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
// Both GetRequestCompletedByID and GetDeployRequestCompletedByID return the same Result/Err.
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

func (s StubSubgraphClient) GetDeployRequestCompletedByID(_ context.Context, _ common.RequestIdType) (*subgraph.RequestCompleted, error) {
	return s.Result, s.Err
}

func (StubSubgraphClient) GetUserEventsBySubTypes(context.Context, common.ApplicationIdType, []string, int, *big.Int) ([]subgraph.UserEvent, error) {
	return nil, nil
}

func (StubSubgraphClient) GetUserEvents(context.Context, common.ApplicationIdType, string, int, *big.Int) ([]subgraph.UserEvent, error) {
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

// WriteTempConf writes a Config to a temporary wallet.conf and returns the path.
func WriteTempConf(t *testing.T, cfg *app.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, app.SaveConfigToFile(cfg, path))
	return path
}
