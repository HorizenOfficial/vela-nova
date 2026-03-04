package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/horizen-cce-common-go/wasm/types"
	runtimeapp "github.com/horizen-pes-nova/payment-app/app"
	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)

func NewRequestReportCommand(config *app.Config, blockchainClient blockchain.Client) *RequestReportCommand {
	return &RequestReportCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

type RequestReportCommand struct {
	*app.ChainCommand
	maxFeeValue string
	reportType  string
	address     string
}

func (c *RequestReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requestreport",
		Short: `requests a deanonymization report of the Nova app`,
		Long:  `requests a deanonymization report of the Nova app. Use --report-type to select 'balances' (default) or 'tx_history'.`,
		Run: func(cmd *cobra.Command, args []string) {

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

			deanonInstruction := runtimeapp.DeanonymizeInstruction{
				ReportType: c.reportType,
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

			depositAmount := big.NewInt(0)
			requestType := common.Deanonymize
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, encryptedPayload, depositAmount, maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to generate a deanonymization report: %v\n", err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")
			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Deanonymization request failed: %v\n", err)
				return
			}
			fmt.Printf("Deanonymization request completed successfully. Report id: %s_%s\n", NOVA_APPLICATION_ID, requestID)

		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	cmd.Flags().StringVar(&c.reportType, "report-type", "", "Report type: 'balances' (default) or 'tx_history'")
	cmd.Flags().StringVar(&c.address, "address", "", "Address to query (required for tx_history)")
	return cmd
}
