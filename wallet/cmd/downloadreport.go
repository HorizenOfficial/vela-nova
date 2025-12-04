package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
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
			var ctx context.Context
			if cmd != nil {
				ctx = cmd.Context()
			}
			if ctx == nil {
				ctx = context.Background()
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
	client := app.NewAuthorityClient(c.Config.AuthorityServiceURL, c.Config.AuthorityServiceChainID, NOVA_APPLICATION_ID, c.Config.KeySecp)

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
			if err := c.InitChainClient(); err != nil {
				return fmt.Errorf("connecting to rpc node: %w", err)
			}
		}
		defer c.CloseClient()

		decrypted, err := app.DecryptReport(ctx, c.BlockchainClient, c.Config.KeyP521, report)
		if err != nil {
			return err
		}

		dataToWrite, err = json.MarshalIndent(decrypted, "", "  ")
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
