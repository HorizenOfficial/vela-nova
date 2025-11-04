package cmd

import (
	"context"
	"fmt"

	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)


func NewRegisterUserCommand(config *app.Config, blockchainClient blockchain.Client) *RegisterUserCommand {
	return &RegisterUserCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

type RegisterUserCommand struct {
	*app.ChainCommand
}


func (c *RegisterUserCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registeruser",
		Short: `register the association [address, encryption key (P521)] of the wallet into the PES system`,
		Long:  `register the association [address, encryption key (P521)] of the wallet into the PES system`,
		Run: func(cmd *cobra.Command, args []string) {

			if c.BlockchainClient == nil {
				//create blockchain client
				 if err :=c.InitChainClient(); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return 

				 }
			} 
			defer c.CloseClient()

			if c.Config.KeyP521 == nil {
				fmt.Println("Error: P521 key not found in the wallet")
				return
			}
			ctx := context.Background()
			payload := c.Config.KeyP521.PublicKey().Bytes()
			value := big.NewInt(0)
		
			requestType := common.AssociateKey
			requestID, blockNumber, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION,  &NOVA_APPLICATION_ID, requestType, payload, value)
			if err != nil {
				fmt.Printf("Error sending request to register public key: %v", err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")


			result, err := c.WaitForRequestCompleted(requestID, blockNumber, ctx)
			if err != nil {
				fmt.Println(err)
				return
			}
			if result {
				fmt.Println("Public key registered successfully")
			} else {
				fmt.Println("Public key registration failed")
			}


		},
	}
	return cmd
}
