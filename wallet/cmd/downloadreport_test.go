package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func TestDownloadReportWithoutDecryptSavesReport(t *testing.T) {
	// Prepare deterministic data
	reportIDBytes := bytes.Repeat([]byte{0x01}, 32)
	reportIDHex := hex.EncodeToString(reportIDBytes)
	encData := hex.EncodeToString([]byte("hello"))

	// Mock rpc chain ID endpoint
	rpcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		// Return chain ID 42
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"0x2a"}`)
	}))
	defer rpcSrv.Close()

	// Mock authority service
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nonce":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"salt":      "00112233445566778899aabbccddeeff",
				"nonce":     hex.EncodeToString(bytes.Repeat([]byte{0x02}, 32)),
				"timestamp": 123,
			})
		case "/getreport":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"applicationId":   "1",
				"reportId":        reportIDHex,
				"authority":       "0x0000000000000000000000000000000000000001",
				"encryptedReport": encData,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	keySecp, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	cfg := &app.Config{
		KeySecp:             keySecp,
		AuthorityServiceURL: ts.URL,
		RpcUrl:              rpcSrv.URL,
		ApplicationID:       common.ApplicationIdType(1),
	}
	testutil.WriteTempConf(t, cfg)

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "out.json")

	cmd := NewDownloadReportCommand(cfg, nil)
	cmd.reportID = reportIDHex
	cmd.destPath = destPath
	cmd.decrypt = false

	err = cmd.Exec(context.Background())
	require.NoError(t, err)

	data, err := os.ReadFile(destPath)
	require.NoError(t, err)

	var report common.DeanonymizationReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.Equal(t, common.ApplicationIdType(1), report.ApplicationID)
	require.Equal(t, reportIDBytes, report.ReportID[:])
	require.Equal(t, []byte("hello"), report.EncryptedReport)
}
