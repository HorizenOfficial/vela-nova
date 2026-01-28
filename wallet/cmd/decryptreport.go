package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/spf13/cobra"
)

type DecryptReportCommand struct {
	*app.ChainCommand
	filePath string
}

func NewDecryptReportCommand(config *app.Config, blockchainClient blockchain.Client) *DecryptReportCommand {
	cmd := &DecryptReportCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
	return cmd
}

func (c *DecryptReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decryptreport",
		Short: `decrypt a deanonymization report specifying file that contains it`,
		Long:  `decrypt a deanonymization report specifying file that contains it`,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			readJson, err := os.ReadFile(c.filePath)
			if err != nil {
				log.Fatalf("error reading file %s: %v", c.filePath, err)
			}
			report, err := app.ParseDeanonymizationReport(readJson)
			if err != nil {
				log.Fatalf("error parsing report from file %s: %v", c.filePath, err)
			}

			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return
				}
			}
			defer c.CloseClient()

			decrypted, err := app.DecryptReport(ctx, c.BlockchainClient, c.Config.KeyP521, report)
			if err != nil {
				log.Fatalf("error decrypting report: %v", err)
			}

			fmt.Println("Decrypted report:")
			fmt.Printf("Application Id: %s\n", report.ApplicationID)
			fmt.Printf("Report Id: %s\n", report.ReportID)
			fmt.Printf("Request Id: %s\n", decrypted.RequestID)
			fmt.Printf("Report Data: %s\n", string(decrypted.ReportDataBytes))
		},
	}
	cmd.Flags().StringVarP(&c.filePath, "path", "p", "", "The path of the file to decrypt (e.g., /path/to/file.txt).")
	return cmd
}
