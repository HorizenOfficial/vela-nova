package cmd

import (
	"fmt"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)

type DepositCommand struct {
	*app.ChainCommand
	depositAmount string
	maxFeeValue string
}

func NewDepositCommand(config *app.Config, blockchainClient blockchain.Client) *DepositCommand {
	cmd := &DepositCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}

	return cmd
}

func (c *DepositCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit",
		Short: `deposit funds into the PES system`,
		Long:  `deposit funds into the PES system`,
		Run: func(cmd *cobra.Command, args []string) {

			amount, err := app.ParseEtherValue(c.depositAmount)
			if err != nil {
				fmt.Printf("Error: invalid amount: %v\n", err)
				return
			}

			maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
			if err != nil {
				fmt.Printf("Error: invalid max fee amount: %v\n", err)
				return
			}

			ctx := cmd.Context()
			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return

				}
			}
			defer c.CloseClient()
			var payload []byte

			requestType := common.Process
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, payload, amount, maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to deposit amount %s: %v\n", c.depositAmount, err)
				return 
			}

			fmt.Println("Waiting for confirmation from PES")

			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Deposit failed: %v\n", err)
				return
			}

			fmt.Println("Deposit completed successfully")

		},
	}
	cmd.Flags().StringVarP(&c.depositAmount, "amount", "a", "", "The amount of Ether to process (e.g., 1.5 ETH). It can be specified in ETH, Wei or GWei. Eg --amount \"147777 Wei\"")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")

	return cmd
}
