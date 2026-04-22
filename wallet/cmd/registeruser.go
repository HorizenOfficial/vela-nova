package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
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
		Short: `register the association [address, encryption key (P521)] of the wallet into the Vela system`,
		Long:  `register the association [address, encryption key (P521)] of the wallet into the Vela system`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.RequireApplicationID(); err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

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

			if c.Config.KeySecp == nil {
				fmt.Println("Error: secp256k1 key not found in the wallet")
				return
			}
			teePubKey, err := c.BlockchainClient.GetTeePublicKey(ctx)
			if err != nil {
				fmt.Printf("Error retrieving TEE public key: %v\n", err)
				return
			}
			payload, err := BuildAssociateKeyPayloadWithSeed(c.Config.KeyP521, c.Config.KeySecp, teePubKey)
			if err != nil {
				fmt.Printf("Error building payload with seed: %v\n", err)
				return
			}

			requestType := common.AssociateKey
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, requestType, payload, ETH_TOKEN, big.NewInt(0), maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to register public key: %v\n", err)
				return
			}

			fmt.Println("Waiting for confirmation from Vela")

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
