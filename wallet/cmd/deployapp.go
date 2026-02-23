package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/horizen-cce-common-go/wallet/blockchain"
	"github.com/horizen-cce-common-go/wallet/common"
	"github.com/horizen-pes-nova/wallet/app"
	"github.com/spf13/cobra"
)

type DeployAppCommand struct {
	*app.ChainCommand
	maxFeeValue string
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
			ctx := context.Background()
			if cmd != nil && cmd.Context() != nil {
				ctx = cmd.Context()
			}

			maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
			if err != nil {
				fmt.Printf("Error: invalid max fee amount: %v\n", err)
				return
			}

			if err := c.InitChainClient(ctx); err != nil {
				fmt.Printf("Error connecting to rpc node: %v", err)
				return
			}
			defer c.BlockchainClient.Close()

			requestType := common.Deploy

			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, []byte{}, big.NewInt(0), maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to deploy app: %v", err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")

			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Deploy app failed: %v\n", err)
				return
			}
			fmt.Println("Deploy app completed successfully")
		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	return cmd
}
