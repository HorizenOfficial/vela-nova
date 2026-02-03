package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
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
			ctx := context.Background()
			if cmd != nil && cmd.Context() != nil {
				ctx = cmd.Context()
			}
			if c.reportID == "" {
				fmt.Println("Error: report id is required")
				return
			}
			if c.Config.AuthorityServiceURL == "" {
				fmt.Println("Error: AuthorityServiceURL is not configured")
				return
			}
			if c.Config.KeySecp == nil {
				fmt.Println("Error: secp256k1 key not configured")
				return
			}
			if c.destPath == "" {
				c.destPath = filepath.Join(".", c.reportID)
			}

			if err := c.run(ctx); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}

	cmd.Flags().StringVar(&c.reportID, "report-id", "", "Report ID to download (hex)")
	cmd.Flags().StringVar(&c.destPath, "dest", "", "Destination file path (defaults to ./<reportId>)")
	cmd.Flags().BoolVar(&c.decrypt, "decrypt", true, "If true, decrypts the report and stores only the decrypted version")

	return cmd
}

func (c *DownloadReportCommand) run(ctx context.Context) error {
	type decryptedReportOutput struct {
		ApplicationID    common.ApplicationIdType `json:"applicationId"`
		RequestID        common.RequestIdType     `json:"requestId"`
		ReportData       json.RawMessage          `json:"reportData,omitempty"`
		ReportDataBase64 string                   `json:"reportDataBase64,omitempty"`
		Authority        string                   `json:"authority,omitempty"`
		RefundAmount     *common.Big              `json:"refundAmount,omitempty"`
		ApplicationFee   *common.Big              `json:"applicationFee,omitempty"`
	}

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
	chainID := id.Uint64()

	client := app.NewAuthorityClient(c.Config.AuthorityServiceURL, chainID, NOVA_APPLICATION_ID, c.Config.KeySecp)

	nonceResp, err := client.FetchNonce(ctx)
	if err != nil {
		return err
	}

	report, err := client.FetchReport(ctx, c.reportID, nonceResp)
	if err != nil {
		return err
	}

	var dataToWrite []byte
	if c.decrypt {
		if c.BlockchainClient == nil {
			if err := c.InitChainClient(ctx); err != nil {
				return fmt.Errorf("connecting to rpc node: %w", err)
			}
		}
		defer c.CloseClient()

		decrypted, err := app.DecryptReport(ctx, c.BlockchainClient, c.Config.KeyP521, report)
		if err != nil {
			return err
		}

		// Try to render report data as pretty JSON; fall back to base64 if it is not valid JSON.
		output := decryptedReportOutput{
			ApplicationID:  decrypted.ApplicationID,
			RequestID:      decrypted.RequestID,
			Authority:      report.Authority.Hex(),
			RefundAmount:   report.RefundAmount,
			ApplicationFee: report.ApplicationFee,
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
