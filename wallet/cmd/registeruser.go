package cmd

import (
	"context"
	"fmt"
	"time"

	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)

type RegisterUserCommand struct {
	*app.AppCommand
	blockchainClient blockchain.Client
}

func NewRegisterUserCommand(config *app.Config, blockchainClient blockchain.Client) *RegisterUserCommand {
	return &RegisterUserCommand{
		AppCommand: app.NewAppCommand(config),
		blockchainClient: blockchainClient,
	}
}

func (c *RegisterUserCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registeruser",
		Short: `register the association [address, encryption key (P521)] of the wallet into the PES system`,
		Long:  `register the association [address, encryption key (P521)] of the wallet into the PES system`,
		Run: func(cmd *cobra.Command, args []string) {

			if c.blockchainClient == nil {
				//create blockchain client
				c.blockchainClient = blockchain.NewBlockChainClient(c.Config.ProcessorEndpointAddress, c.Config.TeeAuthenticatorAddress, c.Config.RpcUrl, &c.Config.KeySecp)
				err := c.blockchainClient.Connect(context.Background())
				if err != nil {
					fmt.Printf("Error connecting to rpc node: %v", err)
					return
				}
			} 
			defer c.blockchainClient.Close()

			payload := c.Config.KeyP521.PublicKey().Bytes()
			value := big.NewInt(0)
			appId := big.NewInt(1)
			var protocolVersion uint8 = 0
			requestType := common.AssociateKey
			requestID, blockNumber, err := c.blockchainClient.SubmitRequest(context.Background(), protocolVersion, appId, requestType, payload, value)
			if err != nil {
				fmt.Printf("Error sending request to register public key: %v", err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")


			ticker := time.NewTicker(time.Duration(c.Config.BlockchainPollingInterval) * time.Second)
			defer ticker.Stop()

			timeoutCh := time.After(time.Duration(c.Config.BlockchainPollingTimeout) * time.Second)

			toBlock := blockNumber + 1
			for {
				select {
				case <-ticker.C:
					result, err := c.blockchainClient.GetRequestCompletedEvent(context.Background(), requestID, 0, toBlock)
					if err != nil {
						fmt.Printf("Error getting request completion event: %v. Retrying", err)
						continue
					}
					if result == nil {
						fmt.Println("Waiting for confirmation from PES...")
						continue
					}
					if result.Status != common.RequestResultOK {
						fmt.Println("Public key registration failed")
						return
					}
					fmt.Println("Public key registered successfully")
					return

				case <-timeoutCh:
					fmt.Println("Timeout expired while waiting for confirmation from PES")
					return
				}
			}

		},
	}
	return cmd
}
