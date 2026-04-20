package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
)

type ClaimPendingPaymentsCommand struct {
	*app.ChainCommand
	token string
}

func NewClaimPendingPaymentsCommand(config *app.Config, blockchainClient blockchain.Client) *ClaimPendingPaymentsCommand {
	return &ClaimPendingPaymentsCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

// Exec runs the claimpendingpayments flow outside of the Cobra wrapper.
// Exported so test drivers can invoke it directly after setting flag-bound
// struct fields.
func (c *ClaimPendingPaymentsCommand) Exec(ctx context.Context) error {
	if c.Config.KeySecp == nil {
		return fmt.Errorf("Secp256k1 key not found in the wallet")
	}

	tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
	if err != nil {
		return err
	}

	if err := c.InitChainClient(ctx); err != nil {
		return fmt.Errorf("connecting to rpc node: %w", err)
	}
	defer c.CloseClient()

	address := ethCommon.HexToAddress(c.Config.KeySecp.PublicKey().Address())

	amount, err := c.BlockchainClient.GetPendingClaims(ctx, tokenInfo.Address, address)
	if err != nil {
		return fmt.Errorf("retrieving pending claims: %w", err)
	}
	if amount.Cmp(big.NewInt(0)) == 0 {
		fmt.Printf("Nothing to claim for %s\n", tokenInfo.Symbol)
		return nil
	}

	fmt.Printf("Claiming %s...\n", c.Config.Tokens.FormatAmount(amount, tokenInfo))
	if err := c.BlockchainClient.Claim(ctx, tokenInfo.Address, address); err != nil {
		return fmt.Errorf("claiming pending payments: %w", err)
	}
	fmt.Println("Pending payments claimed successfully")
	return nil
}

func (c *ClaimPendingPaymentsCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claimpendingpayments",
		Short: `claim pending payments to your public address (amounts coming from fee refunds or withdrawals not yet claimed)`,
		Long:  `claim pending payments from the ProcessorEndpoint contract to your public address`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
