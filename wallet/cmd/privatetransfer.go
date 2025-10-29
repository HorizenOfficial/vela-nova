package cmd

import (
	"fmt"
	"context"
	"log"
	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/crypto"

)

type PrivateTransferCommand struct {
	*app.ChainCommand
	receiver string
	value string
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
			if !ethCommon.IsHexAddress(c.receiver) {
				log.Fatalf("Error: invalid receiver: %s\n", c.receiver)
			}

			//get amount
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

			//build body with type transfer
			jsonBody := `{"type":"trasfer", "transfer": {"amount":"` + amount.String() + `, "to": "` + c.receiver +`"}}`
			//get public key
			publicKey, err := c.BlockchainClient.GetTeePublicKey(context.Background())
			if err != nil {
				log.Fatalf("Error retrieving public key to encrypt: %v", err)
			}
			// encrypt
			payload, err := crypto.Encrypt(c.Config.KeyP521, publicKey, []byte(jsonBody))
			if err != nil {
				log.Fatalf("Error encrypting private transfer payload: %v", err)
			}

			//submit request
			ctx := context.Background()
			requestType := common.Process
			requestID, blockNumber, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, &NOVA_APPLICATION_ID, requestType, payload, big.NewInt(0))
			if err != nil {
				fmt.Printf("Error sending request to trasnfer amount %s to %s: %v", c.value, c.receiver, err)
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
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount of Ether to process (e.g., 1.5 ETH). It can be specified in ETH, Wei or GWei. Eg --amount 147777 Wei")
	cmd.Flags().StringVarP(&c.receiver, "to", "t", "", "The receiver address of the private transfer (.e.g., 0xabc123...)")
	return cmd
}

