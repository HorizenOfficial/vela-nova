package cmd

import (
	"context"
	"fmt"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/spf13/cobra"
)

type DepositCommand struct {
	*app.ChainCommand
	depositAmount string
	maxFeeValue   string
	token         string
}

func NewDepositCommand(config *app.Config, blockchainClient blockchain.Client) *DepositCommand {
	return &DepositCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

// Exec runs the deposit flow outside of the Cobra wrapper. Exported so test
// drivers can invoke it directly after setting flag-bound struct fields.
func (c *DepositCommand) Exec(ctx context.Context) error {
	if err := c.RequireApplicationID(); err != nil {
		return err
	}

	tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
	if err != nil {
		return err
	}

	amount, err := parseAssetAmount(c.Config.Tokens, tokenInfo, c.depositAmount)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
	if err != nil {
		return fmt.Errorf("invalid max fee amount: %w", err)
	}

	if err := c.InitChainClient(ctx); err != nil {
		return fmt.Errorf("connecting to rpc node: %w", err)
	}
	defer c.CloseClient()

	// Print reminder for ERC-20 deposits
	if tokenInfo.Address != ETH_TOKEN {
		fmt.Printf("Note: ensure you have approved the ProcessorEndpoint contract to spend %s %s\n",
			c.depositAmount, tokenInfo.Symbol)
	}

	var payload []byte
	requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, common.Process, payload, tokenInfo.Address, amount, maxFeeValue)
	if err != nil {
		return fmt.Errorf("sending request to deposit %s %s: %w", c.depositAmount, tokenInfo.Symbol, err)
	}

	fmt.Println("Waiting for confirmation from Vela")
	if err := c.WaitForRequestCompleted(requestID, ctx); err != nil {
		return fmt.Errorf("Deposit failed: %w", err)
	}
	fmt.Println("Deposit completed successfully")
	return nil
}

func (c *DepositCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit",
		Short: `deposit funds into the Vela system`,
		Long:  `deposit funds into the Vela system`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.depositAmount, "amount", "a", "", "The amount to deposit (e.g., '1.5 ETH', '100' for ERC-20)")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (always ETH)")
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
