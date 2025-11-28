package cmd

import (
	"fmt"
	"context"
	"log"
	"math/big"

	runtimeapp "github.com/horizen-pes-nova/payment-app/app"
	"github.com/horizen-pes-nova/wallet/app"
	"github.com/spf13/cobra"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
)

type PrivateTransferCommand struct {
	*app.ChainCommand
	receiver string
	value string
	maxFeeValue string
}


func NewPrivateTransferCommand(config *app.Config, blockchainClient blockchain.Client) *PrivateTransferCommand {
	cmd := &PrivateTransferCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}

	return cmd
}

func (c *PrivateTransferCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "privatetransfer",
		Short: `submits a private transfer request specifying receiver address and amount as parameters`,
		Long: `submits a private transfer request specifying receiver address and amount as parameters`,
		Run: func(cmd *cobra.Command, args []string) {
			//get receiver
			to, err := app.ValidateAndChecksumAddress(c.receiver)
			if err != nil {
				log.Fatalf("Error: invalid receiver: %s\n", c.receiver)
			}

			//get amount
			amount, err := app.ParseEtherValue(c.value)
			if err != nil {
				fmt.Printf("Error: invalid amount: %v\n", err)
				return
			}

			maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
			if err != nil {
				fmt.Printf("Error: invalid max fee amount: %v\n", err)
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

			//build payload with type transfer
			payload := runtimeapp.PayloadInstructions{
				Type:     "transfer",
				Transfer: &runtimeapp.TransferInstruction{To: to, Amount: amount},
			}			
			ctx := context.Background()
			encryptedPayload, err := c.EncryptPayload(&payload, ctx)			
			if err != nil {
				log.Fatalf("Error encrypting private transfer payload: %v", err)
			}

			//submit request
			requestType := common.Process
			requestID, blockNumber, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, encryptedPayload, big.NewInt(0), maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to transfer amount %s to %s: %v", c.value, to, err)
				return 
			}
			fmt.Printf("Waiting for confirmation from PES for requestID: %s\n", requestID)
			err = c.WaitForRequestCompleted(requestID, blockNumber, ctx)
			if err != nil {
				fmt.Printf("Private transfer failed: %v\n", err)
				return
			}
			
			fmt.Println("Private transfer completed successfully")
		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount of Ether to process (e.g., 1.5 ETH). It can be specified in ETH, Wei or GWei. Eg --amount 147777 Wei")
	cmd.Flags().StringVarP(&c.receiver, "to", "t", "", "The receiver address of the private transfer. Eg --to 0xabc123...")
	cmd.Flags().StringVar(&c.maxFeeValue, "max-value-fee", "", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	_ = cmd.MarkFlagRequired("max-value-fee")
	return cmd
}

