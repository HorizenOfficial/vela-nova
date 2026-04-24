package main_test

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-nova/payment-app/testhelpers"
	"github.com/HorizenOfficial/vela/pkg/common"
	commontestutil "github.com/HorizenOfficial/vela/pkg/common/testutil"
	"github.com/HorizenOfficial/vela/pkg/executor"
	systemTests "github.com/HorizenOfficial/vela/pkg/testutil"
	"github.com/HorizenOfficial/vela/pkg/testutil/fullstack"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// createFullstackUser creates a funded on-chain account and registers its
// signing key with the CryptoHelper so that seed computation and payload
// encryption work correctly.
func createFullstackUser(t *testing.T, suite *fullstack.FullStackSystemTestSuite, cryptoHelper *systemTests.CryptoHelper) (ethCommon.Address, error) {
	t.Helper()
	addr, signingKey, err := suite.CreateFundedAccount()
	if err != nil {
		return ethCommon.Address{}, err
	}
	cryptoHelper.RegisterUserSigningKey(addr, signingKey)
	return addr, nil
}

// TestFullStackDeployAndDeposit is the fullstack smoke test:
// deploy an app on the real simulated chain, register a user, deposit ETH,
// verify the event is received, and query the in-process subgraph for
// request completions and user events. This proves the entire stack works
// end-to-end: on-chain submission → manager polls chain → executor processes
// WASM → manager submits state update on-chain → events and subgraph data
// observed in test.
func TestFullStackDeployAndDeposit(t *testing.T) {
	if os.Getenv("CI_FLAG") != "" {
		t.Skip("Skipping fullstack test in CI environment")
	}

	t.Setenv("MANAGER_ARTIFACTS_PATH", t.TempDir())

	suite := fullstack.NewFullStackSystemTestSuite(t, "wasmtime-payment", testhelpers.NewNetworkLogConfig(), testhelpers.NewNetworkLogConfig())
	defer suite.Cleanup()

	wasmBytecode := testhelpers.BuildAndLoadWasmModule(t)

	require.NoError(t, suite.StartExecutor())
	require.NoError(t, suite.StartManager())

	timeout := 100 * time.Second

	cryptoHelper := systemTests.NewCryptoHelper()

	// Create funded on-chain account for the user
	userAddress, err := createFullstackUser(t, suite, cryptoHelper)
	require.NoError(t, err)

	// Deploy the app — appID is assigned by the contract.
	// The ProcessorEndpoint contract requires the sender to have the deployer role,
	// so we use the pre-authorized deployer account from the simulated chain.
	deployDescriptor := testhelpers.UploadArtifactAndBuildDescriptorPayload(t, suite, wasmBytecode)
	deployReq := &common.Request{
		RequestType: common.Deploy,
		Payload:     deployDescriptor,
		Sender:      suite.GetDeployerAddress(),
		Timestamp:   common.ToBig(new(big.Int).SetInt64(time.Now().Unix())),
		AssetAmount: common.NewBig(0),
		MaxFeeValue: common.NewBig(100),
	}
	require.NoError(t, suite.SubmitRequest(deployReq))
	appID := deployReq.ApplicationID
	t.Logf("Contract assigned appID: %d", appID)

	_, err = suite.WaitForAppStateInDB(appID, timeout)
	require.NoError(t, err)
	_, err = suite.WaitForAppStateInBlockchain(appID, timeout)
	require.NoError(t, err)

	// Get executor public key for encryption
	executorPubKey, err := suite.GetExecutorCommunicationKey()
	require.NoError(t, err)

	// Register user key (seed-registered)
	testhelpers.RegisterSeedUser(t, suite, cryptoHelper, executorPubKey, appID, userAddress, timeout)

	// Deposit 2 ETH
	depositAmount := big.NewInt(2_000_000_000_000_000_000)
	depositReq, err := cryptoHelper.CreateTokenDepositRequest(appID, commontestutil.GenerateRandomRequestID(), userAddress, ethCommon.Address{}, depositAmount, executorPubKey)
	require.NoError(t, err)
	require.NoError(t, suite.SubmitRequest(depositReq))
	require.NoError(t, suite.AssertRequestCompleted(depositReq.RequestID, timeout))

	// Verify deposit event received
	userSeed, err := cryptoHelper.ComputeSeed(userAddress)
	require.NoError(t, err)
	depositEvent, err := suite.WaitForEventBySubtypes(userAddress, executor.AllSubtypes(userSeed, executor.DefaultSubtypeN), timeout)
	require.NoError(t, err)

	// Decrypt and validate event
	decryptedData, err := cryptoHelper.DecryptEvent(userAddress, depositEvent, executorPubKey)
	require.NoError(t, err)
	var eventData struct {
		Type    string      `json:"type"`
		Amount  *common.Big `json:"amount"`
		Balance *common.Big `json:"balance"`
	}
	require.NoError(t, json.Unmarshal(decryptedData, &eventData))
	require.Equal(t, "deposit", eventData.Type)
	require.Equal(t, 0, depositAmount.Cmp(eventData.Amount.ToInt()),
		"deposit event amount: expected %s, got %s", depositAmount, eventData.Amount.ToInt())

	// Verify signature on the update payload
	executorSigningKey, err := suite.GetExecutorSigningKey()
	require.NoError(t, err)
	payload, err := suite.GetRequestUpdatePayload(depositReq.RequestID)
	require.NoError(t, err)
	require.NoError(t, cryptoHelper.ValidateUpdatePayloadSignature(payload, executorSigningKey))

	// --- In-process subgraph verification ---
	// Query the subgraph for the completed deposit request
	sgClient := suite.GetSubgraphClient()

	depositCompleted, err := sgClient.GetRequestCompletedByID(t.Context(), depositReq.RequestID)
	require.NoError(t, err)
	require.NotNil(t, depositCompleted, "subgraph should have the deposit request completion")
	require.Equal(t, velacommon.RequestResultOK, depositCompleted.Status)
	require.Equal(t, appID, depositCompleted.ApplicationID)
	t.Logf("Subgraph: deposit request completed at block %d", depositCompleted.BlockNumber)

	// Query the subgraph for the deploy request completion
	deployCompleted, err := sgClient.GetDeployRequestCompletedByID(t.Context(), deployReq.RequestID)
	require.NoError(t, err)
	require.NotNil(t, deployCompleted, "subgraph should have the deploy request completion")
	require.Equal(t, velacommon.RequestResultOK, deployCompleted.Status)

	// Query user events from the subgraph — the deposit should have produced an event
	// with the hashed subtype. Query with all subtypes for this user's seed.
	allSubtypes := executor.AllSubtypes(userSeed, executor.DefaultSubtypeN)
	sgEvents, err := sgClient.GetUserEventsBySubTypes(t.Context(), appID, allSubtypes, 10, nil)
	require.NoError(t, err)
	require.NotEmpty(t, sgEvents, "subgraph should have at least one user event after deposit")
	t.Logf("Subgraph: found %d user events for the deposit", len(sgEvents))

	// The most recent event should be the deposit event — decrypt and verify
	sgDecrypted, err := cryptoHelper.DecryptEvent(userAddress, &common.Event{
		ApplicationID: sgEvents[0].ApplicationID,
		UserID:        userAddress,
		EventSubType:  sgEvents[0].EventSubType,
		EncryptedData: sgEvents[0].EncryptedData,
	}, executorPubKey)
	require.NoError(t, err)
	var sgEventData struct {
		Type   string      `json:"type"`
		Amount *common.Big `json:"amount"`
	}
	require.NoError(t, json.Unmarshal(sgDecrypted, &sgEventData))
	require.Equal(t, "deposit", sgEventData.Type)
	require.Equal(t, 0, depositAmount.Cmp(sgEventData.Amount.ToInt()),
		"subgraph event amount: expected %s, got %s", depositAmount, sgEventData.Amount.ToInt())

	suite.AssertNoStateUpdateErrors(t)
	t.Log("Fullstack deploy + deposit + subgraph verification passed")
}

