package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"
)

type DownloadReportCommand struct {
	*app.ChainCommand
	reportID string
	destPath string
	decrypt  bool
}

func NewDownloadReportCommand(config *app.Config, blockchainClient blockchain.Client) *DownloadReportCommand {
	return &DownloadReportCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
		decrypt:      true,
	}
}

func (c *DownloadReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "downloadreport",
		Short: "download a deanonymization report from the authority service",
		Long:  "download a deanonymization report from the authority service, optionally decrypting it after download",
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}

	cmd.Flags().StringVar(&c.reportID, "report-id", "", "Report ID to download (hex)")
	cmd.Flags().StringVar(&c.destPath, "dest", "", "Destination file path (defaults to ./<reportId>)")
	cmd.Flags().BoolVar(&c.decrypt, "decrypt", true, "If true, decrypts the report and stores only the decrypted version")

	return cmd
}

// Exec runs the downloadreport flow outside of the Cobra wrapper. Exported so
// test drivers can invoke it directly after setting flag-bound struct fields.
func (c *DownloadReportCommand) Exec(ctx context.Context) error {
	if err := c.RequireApplicationID(); err != nil {
		return err
	}
	if c.reportID == "" {
		return fmt.Errorf("report id is required")
	}
	if c.Config.AuthorityServiceURL == "" {
		return fmt.Errorf("AuthorityServiceURL is not configured")
	}
	if c.Config.KeySecp == nil {
		return fmt.Errorf("secp256k1 key not configured")
	}
	if c.destPath == "" {
		c.destPath = filepath.Join(".", c.reportID)
	}

	// Two paths for resolving chain ID:
	//   - In-process harness (fullstack tests): BlockchainClient is injected
	//     pre-connected to a simulated backend; we ask it directly. The conf's
	//     RpcUrl is a placeholder and cannot be dialed.
	//   - CLI / unit-tests: no BlockchainClient is injected; fall back to a
	//     lightweight ethclient.Dial on the configured RpcUrl just for chain
	//     id. This keeps CLI behavior unchanged.
	var chainID uint64
	if c.BlockchainClient != nil {
		chainIDBig, err := c.BlockchainClient.ChainID(ctx)
		if err != nil {
			return fmt.Errorf("fetching chain id from injected client: %w", err)
		}
		chainID = chainIDBig.Uint64()
	} else {
		if strings.TrimSpace(c.Config.RpcUrl) == "" {
			return fmt.Errorf("rpcUrl not configured to auto-detect chain ID")
		}
		ethc, err := ethclient.DialContext(ctx, c.Config.RpcUrl)
		if err != nil {
			return fmt.Errorf("dialing rpc to fetch chain id: %w", err)
		}
		defer ethc.Close()
		id, err := ethc.ChainID(ctx)
		if err != nil {
			return fmt.Errorf("fetching chain id: %w", err)
		}
		chainID = id.Uint64()
	}

	authClient := app.NewAuthorityClient(c.Config.AuthorityServiceURL, chainID, c.Config.ApplicationID, c.Config.KeySecp)

	nonceResp, err := authClient.FetchNonce(ctx)
	if err != nil {
		return err
	}

	report, err := authClient.FetchReport(ctx, c.reportID, nonceResp)
	if err != nil {
		return err
	}

	type decryptedReportOutput struct {
		ApplicationID    common.ApplicationIdType `json:"applicationId"`
		RequestID        common.RequestIdType     `json:"requestId"`
		ReportData       json.RawMessage          `json:"reportData,omitempty"`
		ReportDataBase64 string                   `json:"reportDataBase64,omitempty"`
		Authority        string                   `json:"authority,omitempty"`
	}

	var dataToWrite []byte
	if c.decrypt {
		// Decrypt path needs a blockchain client to fetch the TEE public key.
		// If not pre-injected, InitChainClient builds one from Config.
		if err := c.InitChainClient(ctx); err != nil {
			return fmt.Errorf("connecting to rpc node: %w", err)
		}
		defer c.CloseClient()

		decrypted, err := app.DecryptReport(ctx, c.BlockchainClient, c.Config.KeyP521, report)
		if err != nil {
			return err
		}

		// Try to render report data as pretty JSON; fall back to base64 if it is not valid JSON.
		output := decryptedReportOutput{
			ApplicationID: decrypted.ApplicationID,
			RequestID:     decrypted.RequestID,
			Authority:     report.Authority.Hex(),
		}
		if len(decrypted.ReportDataBytes) > 0 {
			var pretty json.RawMessage
			if err := json.Unmarshal(decrypted.ReportDataBytes, &pretty); err == nil {
				output.ReportData = pretty
			} else {
				output.ReportDataBase64 = base64.StdEncoding.EncodeToString(decrypted.ReportDataBytes)
			}
		}

		dataToWrite, err = json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding decrypted report: %w", err)
		}
	} else {
		dataToWrite, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding report: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(c.destPath), 0o755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}
	if err := os.WriteFile(c.destPath, dataToWrite, 0o644); err != nil {
		return fmt.Errorf("writing report to file: %w", err)
	}

	fmt.Printf("Report saved to %s\n", c.destPath)
	return nil
}
