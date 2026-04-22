package cmd

import (
	"context"
	"fmt"
	"math/big"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
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

func (c *ClaimPendingPaymentsCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claimpendingpayments",
		Short: `claim pending payments to your public address (amounts coming from fee refunds or withdrawals not yet claimed)`,
		Long:  `claim pending payments from the ProcessorEndpoint contract to your public address`,
		Run: func(cmd *cobra.Command, args []string) {
			if c.Config.KeySecp == nil {
				fmt.Println("Error: Secp256k1 key not found in the wallet")
				return
			}

			// Resolve token
			tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			ctx := context.Background()
			if cmd != nil && cmd.Context() != nil {
				ctx = cmd.Context()
			}
			if c.BlockchainClient == nil {
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return
				}
			}
			defer c.CloseClient()

			address := ethCommon.HexToAddress(c.Config.KeySecp.PublicKey().Address())

			amount, err := c.BlockchainClient.GetPendingClaims(ctx, tokenInfo.Address, address)
			if err != nil {
				fmt.Printf("Error retrieving pending claims: %v\n", err)
				return
			}
			if amount.Cmp(big.NewInt(0)) == 0 {
				fmt.Printf("Nothing to claim for %s\n", tokenInfo.Symbol)
				return
			}

			fmt.Printf("Claiming %s...\n", c.Config.Tokens.FormatAmount(amount, tokenInfo))

			err = c.BlockchainClient.Claim(ctx, tokenInfo.Address, address)
			if err != nil {
				fmt.Printf("Error claiming pending payments: %v\n", err)
				return
			}

			fmt.Println("Pending payments claimed successfully")
		},
	}
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
