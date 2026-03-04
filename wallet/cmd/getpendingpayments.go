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
			amount, err := c.BlockchainClient.GetPendingPayments(ctx, address)
			if err != nil {
				fmt.Printf("Error retrieving pending payments: %v\n", err)
				return
			}

			fmt.Printf("Pending payments: %s ETH\n", app.WeiToEtherStr(amount))
		},
	}
	return cmd
}
