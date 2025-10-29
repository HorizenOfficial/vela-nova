package cmd

import (
	"bytes"
	"context"
	"io"
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/horizen-pes/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// create a test blockchain client with only the GetUserEvents method defined
type TestPrivateTransferBlockChainClient struct {
	blockchain.MockClient
	request *common.Request
}

///rewrite SubmitRequest
func (c *TestPrivateTransferBlockChainClient) SubmitRequest(ctx context.Context, protocolVersion uint8, applicationId *big.Int, requestType common.RequestType, payload []byte, value *big.Int) (string, uint64, error) {
	c.request = &common.Request{
		ProtocolVersion: strconv.FormatUint(uint64(protocolVersion), 10),		
		ApplicationID:   applicationId.String(),
		RequestType:     requestType,
		Payload:         payload,
		Value:           value.Uint64(),
	}
	return "1", 0, nil
}

func TestPrivateTransfer(t *testing.T) {

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	client := &TestPrivateTransferBlockChainClient{
		*blockchain.NewMockClient(),
		nil,
	}

	// Execute the command
	cmd := NewPrivateTransferCommand(&app.Config{
		KeySecp: key1,
		KeyP521: key2,
	}, client).Command()
	cmd.Flags().Set("amount", "1")
	cmd.Flags().Set("to", "0x0000000000000000000000000000000000000001")

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, "Private transfer request submitted with request id:")

	//check that request is saved correctly
	require.Equal(t, client.request.RequestType, common.Process)
	require.Greater(t, len(client.request.Payload), 0)
	require.Equal(t, client.request.ProtocolVersion, strconv.FormatUint(uint64(PROTOCOL_VERSION), 10))
	require.Equal(t, client.request.ApplicationID, NOVA_APPLICATION_ID.String())
	require.Equal(t, client.request.Value, uint64(0))

}
