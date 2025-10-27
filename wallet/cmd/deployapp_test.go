package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDeployAppCommand_Success(t *testing.T) {

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	testHelper := testutil.NewSimTestHelper(t, true, true, nil, nil)
	defer testHelper.Close()

	blockchainClient := SetupNewBlockChainClient(testHelper)

	config := &app.Config{
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  60,
	}

	deployCmd := NewDeployAppCommand(config, blockchainClient)
	cmd := deployCmd.Command()

	go completeNextRequest(t, testHelper)
	cmd.Run(nil, []string{"1"})

	// Restore stdout
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	assert.NoError(t, err)

	assert.Contains(t, buf.String(), "Deploy app completed successfully")
}
