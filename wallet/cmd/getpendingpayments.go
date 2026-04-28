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

type GetPendingPaymentsCommand struct {
	*app.ChainCommand
	token string
}

func NewGetPendingPaymentsCommand(config *app.Config, blockchainClient blockchain.Client) *GetPendingPaymentsCommand {
	return &GetPendingPaymentsCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

// Exec runs the getpendingpayments flow outside of the Cobra wrapper. Returns
// (amount, tokenInfo, err) so test drivers can assert on the numeric pending
// amount; tokenInfo is returned alongside so the Cobra wrapper doesn't need to
// re-resolve the token just to format the output.
func (c *GetPendingPaymentsCommand) Exec(ctx context.Context) (*big.Int, *app.TokenInfo, error) {
	if c.Config.KeySecp == nil {
		return nil, nil, fmt.Errorf("Secp256k1 key not found in the wallet")
	}

	tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
	if err != nil {
		return nil, nil, err
	}

	if err := c.InitChainClient(ctx); err != nil {
		return nil, tokenInfo, fmt.Errorf("connecting to rpc node: %w", err)
	}
	defer c.CloseClient()

	address := ethCommon.HexToAddress(c.Config.KeySecp.PublicKey().Address())
	amount, err := c.BlockchainClient.GetPendingClaims(ctx, tokenInfo.Address, address)
	if err != nil {
		return nil, tokenInfo, fmt.Errorf("retrieving pending claims: %w", err)
	}
	return amount, tokenInfo, nil
}

func (c *GetPendingPaymentsCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getpendingpayments",
		Short: `display pending payments for your public address (amounts coming from fee refunds or withdrawals not yet claimed)`,
		Long:  `display pending payments for this wallet address (amounts coming from fee refunds or withdrawals not yet claimed) on the ProcessorEndpoint contract`,
		Run: func(cmd *cobra.Command, args []string) {
			amount, tokenInfo, err := c.Exec(resolveContext(cmd))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			fmt.Printf("Pending claims: %s\n", c.Config.Tokens.FormatAmount(amount, tokenInfo))
		},
	}
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
