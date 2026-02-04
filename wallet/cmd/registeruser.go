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
	maxFeeValue string
}

func (c *RegisterUserCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registeruser",
		Short: `register the association [address, encryption key (P521)] of the wallet into the PES system`,
		Long:  `register the association [address, encryption key (P521)] of the wallet into the PES system`,
		Run: func(cmd *cobra.Command, args []string) {

			maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
			if err != nil {
				fmt.Printf("Error: invalid max fee amount: %v\n", err)
				return
			}

			ctx := context.Background()
			if cmd != nil && cmd.Context() != nil {
				ctx = cmd.Context()
			}
			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return

				}
			}
			defer c.CloseClient()

			if c.Config.KeyP521 == nil {
				fmt.Println("Error: P521 key not found in the wallet")
				return
			}
			payload := c.Config.KeyP521.PublicKey().Bytes()

			depositAmount := big.NewInt(0)

			requestType := common.AssociateKey
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, payload, depositAmount, maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to register public key: %v\n", err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")

			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Register user failed: %v\n", err)
				return
			}
			fmt.Println("Public key registered successfully")

		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	return cmd
}
