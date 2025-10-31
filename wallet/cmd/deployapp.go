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

type DeployAppCommand struct {
	*app.ChainCommand
}

func NewDeployAppCommand(config *app.Config, blockchainClient blockchain.Client) *DeployAppCommand {
	return &DeployAppCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

func (c *DeployAppCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployapp",
		Short: `Triggers the app deployment (admin feature).  Note: the app deployment is permissioned for now - wasm is not sent with this command and must be provided to the admins offchain in advance`,
		Long:  `Triggers the app deployment (admin feature).  Note: the app deployment is permissioned for now - wasm is not sent with this command and must be provided to the admins offchain in advance`,
		Run: func(cmd *cobra.Command, args []string) {

			if c.BlockchainClient == nil {
				//create blockchain client
				c.BlockchainClient = blockchain.NewBlockChainClient(*c.Config.ProcessorEndpointAddress, *c.Config.TeeAuthenticatorAddress, c.Config.RpcUrl, c.Config.KeySecp)
				err := c.BlockchainClient.Connect(context.Background())
				if err != nil {
					fmt.Printf("Error connecting to rpc node: %v", err)
					return
				}
			}
			defer c.BlockchainClient.Close()
			ctx := context.Background()
			var protocolVersion uint8 = 0
			requestType := common.Deploy

			requestID, blockNumber, err := c.BlockchainClient.SubmitRequest(context.Background(), protocolVersion, big.NewInt(1), requestType, []byte{}, big.NewInt(0))
			if err != nil {
				fmt.Printf("Error sending request to deploy app: %v", err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")

			result, err := c.WaitForRequestCompleted(requestID, blockNumber, ctx)
			if err != nil {
				fmt.Println(err)
				return
			}
			if result {
				fmt.Println("Deploy app completed successfully")
			} else {
				fmt.Println("Deploy app failed")
			}
		},
	}
	return cmd
}
