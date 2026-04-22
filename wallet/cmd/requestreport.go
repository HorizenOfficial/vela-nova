package cmd

import (
	"context"
	"fmt"
	"math/big"

	runtimeapp "github.com/HorizenOfficial/vela-nova/payment-app/app"
	"github.com/HorizenOfficial/vela-common-go/wasm/types"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/spf13/cobra"
)

func NewRequestReportCommand(config *app.Config, blockchainClient blockchain.Client) *RequestReportCommand {
	return &RequestReportCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

type RequestReportCommand struct {
	*app.ChainCommand
	maxFeeValue   string
	reportType    string
	address       string
	fromTimestamp int64
	toTimestamp   int64
}

func (c *RequestReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requestreport",
		Short: `requests a deanonymization report of the Nova app`,
		Long:  `requests a deanonymization report of the Nova app. Use --report-type to select 'balances' (default) or 'tx_history'.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.RequireApplicationID(); err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
			if err != nil {
				fmt.Printf("Error: invalid max fee amount: %v\n", err)
				return
			}

			ctx := context.Background()
			if cmd != nil && cmd.Context() != nil {
				ctx = cmd.Context()
			}
			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return
				}
			}
			defer c.CloseClient()

			if c.reportType != "" && c.reportType != "balances" && c.reportType != "tx_history" {
				fmt.Printf("Error: unsupported report type %q (must be 'balances' or 'tx_history')\n", c.reportType)
				return
			}
			if c.reportType == "tx_history" && c.address == "" {
				fmt.Println("Error: --address is required for tx_history report type")
				return
			}

			deanonInstruction := runtimeapp.DeanonymizeInstruction{
				ReportType:    c.reportType,
				FromTimestamp: c.fromTimestamp,
				ToTimestamp:   c.toTimestamp,
			}
			if c.address != "" {
				addr, err := types.HexToAddress(c.address)
				if err != nil {
					fmt.Printf("Error: invalid address: %v\n", err)
					return
				}
				deanonInstruction.Address = addr
			}

			payload := runtimeapp.PayloadInstructions{Deanonymize: &deanonInstruction}
			encryptedPayload, err := c.EncryptPayload(&payload, ctx)
			if err != nil {
				fmt.Printf("Error encrypting deanonymization payload: %v\n", err)
				return
			}

			requestType := common.Deanonymize
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, requestType, encryptedPayload, ETH_TOKEN, big.NewInt(0), maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to generate a deanonymization report: %v\n", err)
				return
			}

			fmt.Println("Waiting for confirmation from Vela")
			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Deanonymization request failed: %v\n", err)
				return
			}
			fmt.Printf("Deanonymization request completed successfully. Report id: %d_%s\n", c.Config.ApplicationID, requestID)

		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	cmd.Flags().StringVar(&c.reportType, "report-type", "", "Report type: 'balances' (default) or 'tx_history'")
	cmd.Flags().StringVar(&c.address, "address", "", "Address to query (required for tx_history)")
	cmd.Flags().Int64Var(&c.fromTimestamp, "from-timestamp", 0, "Filter tx_history from this Unix timestamp (inclusive)")
	cmd.Flags().Int64Var(&c.toTimestamp, "to-timestamp", 0, "Filter tx_history up to this Unix timestamp (inclusive)")
	return cmd
}
