package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes-nova/wallet/cmd/testutil"
	pestestutil "github.com/horizen-pes/pkg/blockchain/testutil"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"

)

func TestDecryptReport(t *testing.T) {
	// create keys
	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	// generate encrypted payload
	payload := "test report 12345"
	encrypted, err := crypto.Encrypt(teeKey, key2.PublicKey(), []byte(payload))
	require.NoError(t, err)
	// write to file
	tmpFile, err := os.CreateTemp("", "testreport-*.txt")
	require.NoError(t, err)
	filePath := tmpFile.Name()
	defer os.Remove(filePath)
	
	err = os.WriteFile(filePath, encrypted, 0644)
	require.NoError(t, err)

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	//create client
	testHelper := pestestutil.NewSimTestHelper(t, true, true, nil, teeKey.PublicKey().Bytes())
	defer testHelper.Close()
	client := testutil.SetupNewBlockChainClient(testHelper)

	// Execute the command
	cmd := NewDecryptReportCommand(&app.Config{
		KeySecp: key1,
		KeyP521: key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}, client).Command()
	cmd.Flags().Set("path", filePath)
	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, payload)
}
