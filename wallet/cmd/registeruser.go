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

// Exec runs the registeruser flow outside of the Cobra wrapper. Exported so
// test drivers can invoke it directly after setting flag-bound struct fields.
func (c *RegisterUserCommand) Exec(ctx context.Context) error {
	if err := c.RequireApplicationID(); err != nil {
		return err
	}

	maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
	if err != nil {
		return fmt.Errorf("invalid max fee amount: %w", err)
	}

	if err := c.InitChainClient(ctx); err != nil {
		return fmt.Errorf("connecting to rpc node: %w", err)
	}
	defer c.CloseClient()

	if c.Config.KeyP521 == nil {
		return fmt.Errorf("P521 key not found in the wallet")
	}
	if c.Config.KeySecp == nil {
		return fmt.Errorf("secp256k1 key not found in the wallet")
	}

	teePubKey, err := c.BlockchainClient.GetTeePublicKey(ctx)
	if err != nil {
		return fmt.Errorf("retrieving TEE public key: %w", err)
	}

	payload, err := BuildAssociateKeyPayloadWithSeed(c.Config.KeyP521, c.Config.KeySecp, teePubKey)
	if err != nil {
		return fmt.Errorf("building payload with seed: %w", err)
	}

	requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, common.AssociateKey, payload, ETH_TOKEN, big.NewInt(0), maxFeeValue)
	if err != nil {
		return fmt.Errorf("sending request to register public key: %w", err)
	}

	fmt.Println("Waiting for confirmation from Vela")
	if err := c.WaitForRequestCompleted(requestID, ctx); err != nil {
		return fmt.Errorf("Register user failed: %w", err)
	}
	fmt.Println("Public key registered successfully")
	return nil
}

func (c *RegisterUserCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registeruser",
		Short: `register the association [address, encryption key (P521)] of the wallet into the Vela system`,
		Long:  `register the association [address, encryption key (P521)] of the wallet into the Vela system`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	return cmd
}
