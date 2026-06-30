package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-common-go/subgraph"
	"github.com/HorizenOfficial/vela-common-go/subtypes"

	"github.com/HorizenOfficial/vela/pkg/common/testutil"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/stretchr/testify/require"
)

// asciiSubType packs a short ASCII tag into a [32]byte for readable test literals.
func asciiSubType(s string) [32]byte {
	var b [32]byte
	copy(b[:], s)
	return b
}

func withUserEventsPageSize(t *testing.T, size int) {
	t.Helper()
	old := userEventsPageSize
	userEventsPageSize = size
	t.Cleanup(func() {
		userEventsPageSize = old
	})
}

func TestFetchAndDecryptUserEvents_LimitOne(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(1)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("msg-1"))
	require.NoError(t, err)
	ev2Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("msg-2"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2Cipher, EventSubType: asciiSubType("a"), BlockNumber: 3},
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1Cipher, EventSubType: asciiSubType("a"), BlockNumber: 2},
	})

	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, nil, 1, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("msg-2"), result[0])
}

func TestFetchAndDecryptUserEvents_Filter(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(2)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("keep-this"))
	require.NoError(t, err)
	ev2Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("drop-this"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1Cipher, EventSubType: asciiSubType("b"), BlockNumber: 5},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2Cipher, EventSubType: asciiSubType("b"), BlockNumber: 4},
	})

	filter := func(data []byte) bool {
		return strings.Contains(string(data), "keep")
	}

	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, nil, 10, filter)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("keep-this"), result[0])
}

func TestFetchAndDecryptUserEvents_UserSpecificDecryption(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKeyA, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKeyB, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(3)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1Cipher, err := crypto.Encrypt(teeKey, userKeyA.PublicKey(), []byte("user-a"))
	require.NoError(t, err)
	ev2Cipher, err := crypto.Encrypt(teeKey, userKeyB.PublicKey(), []byte("user-b"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1Cipher, EventSubType: asciiSubType("c"), BlockNumber: 3},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2Cipher, EventSubType: asciiSubType("c"), BlockNumber: 2},
	})

	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKeyA, appID, nil, 10, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("user-a"), result[0])

	result, err = FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKeyB, appID, nil, 10, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("user-b"), result[0])
}

func TestFetchAndDecryptUserEvents_PaginatesUntilMatch(t *testing.T) {
	withUserEventsPageSize(t, 1)

	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(3)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("skip-me"))
	require.NoError(t, err)
	ev2Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("target"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1Cipher, EventSubType: asciiSubType("c"), BlockNumber: 2},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2Cipher, EventSubType: asciiSubType("c"), BlockNumber: 1},
	})

	filter := func(data []byte) bool {
		return strings.Contains(string(data), "target")
	}

	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, nil, 1, filter)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("target"), result[0])
}

func TestFetchAndDecryptUserEvents_MaxResults(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(4)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("first"))
	require.NoError(t, err)
	ev2Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("second"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1Cipher, EventSubType: asciiSubType("d"), BlockNumber: 2},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2Cipher, EventSubType: asciiSubType("d"), BlockNumber: 1},
	})

	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, nil, 1, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("first"), result[0])
}

func TestFetchAndDecryptUserEvents_NoLimit(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(5)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("one"))
	require.NoError(t, err)
	ev2Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("two"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1Cipher, EventSubType: asciiSubType("e"), BlockNumber: 2},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2Cipher, EventSubType: asciiSubType("e"), BlockNumber: 1},
	})

	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, nil, 0, nil)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, []byte("one"), result[0])
	require.Equal(t, []byte("two"), result[1])
}

func TestFetchAndDecryptUserEvents_OrderWithinBlock(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(6)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("first-log"))
	require.NoError(t, err)
	ev2Cipher, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("second-log"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1Cipher, EventSubType: asciiSubType("f"), BlockNumber: 10, LogIndex: 1},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2Cipher, EventSubType: asciiSubType("f"), BlockNumber: 10, LogIndex: 2},
	})

	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, nil, 1, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("second-log"), result[0])
}

func TestFetchAndDecryptUserEvents_SeedSubTypesFilter(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	secpKey, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	// Generate seed-derived subtypes for this user.
	seed, err := GenerateSeed(secpKey)
	require.NoError(t, err)
	subtypes := EventSubTypesFromSeed(seed, subtypes.DefaultSubtypeN)

	appID := common.NewApplicationId(7)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()
	reqID3 := testutil.GenerateRandomRequestID()

	// One event with a matching seed subtype, one with a non-matching subtype,
	// and one from another user (different encryption key).
	evMatch, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("seed-match"))
	require.NoError(t, err)
	evNoMatch, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("no-match"))
	require.NoError(t, err)
	evOther, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("other-subtype"))
	require.NoError(t, err)

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: evMatch, EventSubType: subtypes[0], BlockNumber: 3},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: evNoMatch, EventSubType: asciiSubType("unknown-subtype"), BlockNumber: 2},
		{ApplicationID: appID, RequestID: reqID3, EncryptedData: evOther, EventSubType: asciiSubType("deposit"), BlockNumber: 1},
	})

	// With seed subtypes: only the matching event is returned.
	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, subtypes, 0, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, []byte("seed-match"), result[0])

	// Without seed subtypes (nil): all decryptable events are returned.
	result, err = FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, nil, 0, nil)
	require.NoError(t, err)
	require.Len(t, result, 3)
}

func TestFetchAndDecryptUserEvents_SingleSubType(t *testing.T) {
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	appID := common.NewApplicationId(8)
	reqID1 := testutil.GenerateRandomRequestID()
	reqID2 := testutil.GenerateRandomRequestID()

	ev1, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("deposit-event"))
	require.NoError(t, err)
	ev2, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte("transfer-event"))
	require.NoError(t, err)

	depositSubType := asciiSubType("deposit")
	transferSubType := asciiSubType("transfer")

	mock := subgraph.NewMockClient().WithUserEvents(appID, []subgraph.UserEvent{
		{ApplicationID: appID, RequestID: reqID1, EncryptedData: ev1, EventSubType: depositSubType, BlockNumber: 2},
		{ApplicationID: appID, RequestID: reqID2, EncryptedData: ev2, EventSubType: transferSubType, BlockNumber: 1},
	})

	// Single subtype passed: matched server-side by the subgraph filter.
	result, err := FetchAndDecryptUserEvents(context.Background(), mock, teeKey.PublicKey(), *userKey, appID, [][32]byte{depositSubType}, 0, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result), 1)
}
