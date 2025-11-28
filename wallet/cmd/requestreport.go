package cmd

import (
	"context"
	"fmt"

	"math/big"

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
}


func (c *RequestReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requestreport",
		Short: `requests a deanonymization report of the balances of Nova app`,
		Long:  `requests a deanonymization report of the balances of Nova app`,
		Run: func(cmd *cobra.Command, args []string) {

			maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
			if err != nil {
				fmt.Printf("Error: invalid max fee amount: %v\n", err)
				return
			}

			if c.BlockchainClient == nil {
				//create blockchain client
				if err :=c.InitChainClient(); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return 
				}
			} 
			defer c.CloseClient()

			payload := runtimeapp.ReportPayloadInstructions{} //empty for now

			ctx := context.Background()
			encryptedPayload, err := c.EncryptPayload(&payload, ctx)
			if err != nil {
				fmt.Printf("Error encrypting deanonymization payload: %v\n", err)
				return 
			}
			
			value := big.NewInt(0)
			requestType := common.Deanonymize
			requestID, blockNumber, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION,  NOVA_APPLICATION_ID, requestType, encryptedPayload, value, maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to generate a deanonymization report: %v\n", err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")
			err = c.WaitForRequestCompleted(requestID, blockNumber, ctx)
			if err != nil {
				fmt.Printf("Deanonymization request failed: %v\n", err)
				return
			}
			fmt.Printf("Deanonymization request completed successfully. Report id: %s_%s\n", NOVA_APPLICATION_ID, requestID)


		},
	}
	cmd.Flags().StringVar(&c.maxFeeValue, "max-value-fee", "", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	_ = cmd.MarkFlagRequired("max-value-fee")
	return cmd
}

