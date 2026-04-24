package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela/pkg/common"
	commontestutil "github.com/HorizenOfficial/vela/pkg/common/testutil"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	pestestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"

)

func TestDecryptReport(t *testing.T) {
	// create keys
	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()

	// create report
	reportFinalJson := "{'accounts': ['0x1111']}"
	//create json object to encrypt
	RequestID := commontestutil.GenerateRandomRequestID()
	payload, err := json.Marshal(
		common.DecryptedReport {
			ApplicationID:  common.NewApplicationId(1),
			RequestID: RequestID,
			ReportDataBytes: []byte(reportFinalJson),
		},
	)
	require.NoError(t, err)

	encrypted, err := crypto.Encrypt(teeKey, key2.PublicKey(), []byte(payload))
	require.NoError(t, err)

	// write to file as json
	tmpFile, err := os.CreateTemp("", "testreport-*.txt")
	require.NoError(t, err)
	filePath := tmpFile.Name()
	defer os.Remove(filePath)

	jsonData, err := json.Marshal(
		common.DeanonymizationReport {
			ApplicationID:  common.NewApplicationId(1),
			ReportID:       RequestID,
			EncryptedReport: encrypted,
		},
	)

	require.NoError(t, err)
	err = os.WriteFile(filePath, jsonData, 0644)
	require.NoError(t, err)

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	//create client
	autoMining, useMockContracts := true, true
	testHelper := pestestutil.NewSimTestHelper(t, autoMining, useMockContracts, nil, teeKey.PublicKey().Bytes())
	defer testHelper.Close()
	client := testutil.SetupNewBlockChainClient(testHelper)

	// Execute the command
	cfg := &app.Config{
		KeySecp:                   key1,
		KeyP521:                   key2,
		BlockchainPollingInterval: 2,
		BlockchainPollingTimeout:  10,
	}
	testutil.WriteTempConf(t, cfg)
	cmd := NewDecryptReportCommand(cfg, client).Command()
	cmd.Flags().Set("path", filePath)
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, reportFinalJson)
}
