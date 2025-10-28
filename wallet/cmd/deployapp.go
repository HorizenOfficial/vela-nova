package cmd

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)

type DeployAppCommand struct {
	*app.AppCommand
	blockchainClient blockchain.Client
}

func NewDeployAppCommand(config *app.Config, blockchainClient blockchain.Client) *DeployAppCommand {
	return &DeployAppCommand{
		AppCommand:       app.NewAppCommand(config),
		blockchainClient: blockchainClient,
	}
}

func (c *DeployAppCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployapp",
		Short: `Triggers the app deployment (admin feature).  Note: the app deployment is permissioned for now - wasm is not sent with this command and must be provided to the admins offchain in advance`,
		Long:  `Triggers the app deployment (admin feature).  Note: the app deployment is permissioned for now - wasm is not sent with this command and must be provided to the admins offchain in advance`,
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

			var protocolVersion uint8 = 0
			requestType := common.Deploy

			requestID, blockNumber, err := c.blockchainClient.SubmitRequest(context.Background(), protocolVersion, big.NewInt(1), requestType, []byte{}, big.NewInt(0))
			if err != nil {
				fmt.Printf("Error sending request to deploy app: %v", err)
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
						fmt.Println("Deploy app failed")
						return
					}
					fmt.Println("Deploy app completed successfully")
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
