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

type DepositCommand struct {
	*app.ChainCommand
	depositAmount string
	maxFeeValue   string
	token         string
}

func NewDepositCommand(config *app.Config, blockchainClient blockchain.Client) *DepositCommand {
	cmd := &DepositCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}

	return cmd
}

func (c *DepositCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit",
		Short: `deposit funds into the Vela system`,
		Long:  `deposit funds into the Vela system`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.RequireApplicationID(); err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			// Resolve token
			tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}

			// Parse amount: ETH uses ParseEtherValue (supports unit suffixes),
			// ERC-20 uses token-aware parsing with the correct decimals.
			var amount *big.Int
			if tokenInfo.Address == ETH_TOKEN {
				amount, err = app.ParseEtherValue(c.depositAmount)
			} else {
				amount, err = c.Config.Tokens.ParseAmount(c.depositAmount, tokenInfo)
			}
			if err != nil {
				fmt.Printf("Error: invalid amount: %v\n", err)
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
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return
				}
			}
			defer c.CloseClient()

			// Print reminder for ERC-20 deposits
			if tokenInfo.Address != ETH_TOKEN {
				fmt.Printf("Note: ensure you have approved the ProcessorEndpoint contract to spend %s %s\n",
					c.depositAmount, tokenInfo.Symbol)
			}

			var payload []byte
			requestType := common.Process
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, requestType, payload, tokenInfo.Address, amount, maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to deposit %s %s: %v\n", c.depositAmount, tokenInfo.Symbol, err)
				return
			}

			fmt.Println("Waiting for confirmation from Vela")

			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Deposit failed: %v\n", err)
				return
			}

			fmt.Println("Deposit completed successfully")

		},
	}
	cmd.Flags().StringVarP(&c.depositAmount, "amount", "a", "", "The amount to deposit (e.g., '1.5 ETH', '100' for ERC-20)")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (always ETH)")
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")

	return cmd
}
