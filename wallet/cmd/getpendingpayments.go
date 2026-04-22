package cmd

import (
	"context"
	"fmt"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/spf13/cobra"
)

type GetPendingPaymentsCommand struct {
	*app.ChainCommand
	token string
}

func NewGetPendingPaymentsCommand(config *app.Config, blockchainClient blockchain.Client) *GetPendingPaymentsCommand {
	return &GetPendingPaymentsCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

func (c *GetPendingPaymentsCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getpendingpayments",
		Short: `display pending payments for your public address (amounts coming from fee refunds or withdrawals not yet claimed)`,
		Long:  `display pending payments for this wallet address (amounts coming from fee refunds or withdrawals not yet claimed) on the ProcessorEndpoint contract`,
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

			fmt.Printf("Pending claims: %s\n", c.Config.Tokens.FormatAmount(amount, tokenInfo))
		},
	}
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
