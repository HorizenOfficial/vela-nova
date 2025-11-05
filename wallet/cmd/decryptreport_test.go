package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
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

	// create report
	reportFinalJson := "{'accounts': ['0x1111']}"
	//base64
	encodedReportFinalJson := base64.StdEncoding.EncodeToString([]byte(reportFinalJson))
	//create json object to encrypt
	payload, err := json.Marshal(
		DecryptedReport {
			ApplicationId:  "1",
			RequestId: "test-request-id",
			ReportDataBytes: encodedReportFinalJson,
		},
	)
	require.NoError(t, err)

	encrypted, err := crypto.Encrypt(teeKey, key2.PublicKey(), []byte(payload))
	require.NoError(t, err)
	//encode as base64
	encoded := base64.StdEncoding.EncodeToString(encrypted)

	// write to file as json
	tmpFile, err := os.CreateTemp("", "testreport-*.txt")
	require.NoError(t, err)
	filePath := tmpFile.Name()
	defer os.Remove(filePath)

	jsonData, err := json.Marshal(
		Report{
			ApplicationId:  "1",
			ReportId:       "test-report-Id",
			EncryptedReport: encoded,
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
	
	assert.Contains(t, output, reportFinalJson)
}
