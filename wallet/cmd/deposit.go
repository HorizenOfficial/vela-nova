package cmd

import (
	"context"
	"fmt"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)


type DepositCommand struct {
	*app.ChainCommand
	value string
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
		Run: func(cmd *cobra.Command, args []string)  {

			amount, err := app.ParseEtherValue(c.value)
			if err != nil {
				fmt.Printf("Error: invalid amount: %v\n", err)
				return
			}
			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return 

				}
			} 
			defer c.CloseClient()

			ctx := context.Background()
			var payload []byte
			
			requestType := common.Process
			requestID, blockNumber, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, payload, amount)
			if err != nil {
				fmt.Printf("Error sending request to deposit amount %s: %v\n", c.value, err)
				return 
			}

			fmt.Println("Waiting for confirmation from PES")


			result, err := c.WaitForRequestCompleted(requestID, blockNumber, ctx)
			if err != nil {
				fmt.Println(err)
				return
			}
			
			if result {
				fmt.Println("Deposit completed successfully")
			} else {
				fmt.Println("Deposit failed")
			}

			
		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount of Ether to process (e.g., 1.5 ETH). It can be specified in ETH, Wei or GWei. Eg --amount \"147777 Wei\"")
	return cmd
}

