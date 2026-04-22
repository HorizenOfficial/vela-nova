package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/HorizenOfficial/vela-common-go/wasm/types"
	runtimeapp "github.com/HorizenOfficial/vela-nova/payment-app/app"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/spf13/cobra"
)

type WithdrawCommand struct {
	*app.ChainCommand
	value       string
	maxFeeValue string
	receiver    string
	token       string
}

func NewWithdrawCommand(config *app.Config, blockchainClient blockchain.Client) *WithdrawCommand {
	cmd := &WithdrawCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}

	return cmd
}

func (c *WithdrawCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw",
		Short: `withdraw funds from the Vela system`,
		Long:  `withdraw funds from the Vela system and send them to a receiver address`,
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

			// Parse amount with correct decimals
			var amount *big.Int
			if tokenInfo.Address == ETH_TOKEN {
				amount, err = app.ParseEtherValue(c.value)
			} else {
				amount, err = c.Config.Tokens.ParseAmount(c.value, tokenInfo)
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

			receiver, err := app.ValidateAndChecksumAddress(c.receiver)
			if err != nil {
				fmt.Printf("Error: invalid receiver address: %v\n", err)
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

			receiverAddr, err := types.HexToAddress(receiver.Hex())
			if err != nil {
				fmt.Printf("Error: invalid receiver address, could not convert to byte array: %v\n", err)
				return
			}

			// Resolve token address for the encrypted payload
			tokenAddr, err := types.HexToAddress(tokenInfo.Address.Hex())
			if err != nil {
				fmt.Printf("Error: invalid token address: %v\n", err)
				return
			}

			payload := runtimeapp.PayloadInstructions{
				Type: "withdraw",
				Withdraw: &runtimeapp.WithdrawInstruction{
					To:           receiverAddr,
					TokenAddress: tokenAddr,
					Amount:       new(types.Uint256).SetBytes(amount.Bytes()),
				},
			}
			encryptedPayload, err := c.EncryptPayload(&payload, ctx)
			if err != nil {
				fmt.Printf("Error encrypting withdraw payload: %v\n", err)
				return
			}

			// On-chain: no business asset deposited (withdrawal is from private state),
			// so tokenAddress=ETH_TOKEN and assetAmount=0.
			requestType := common.Process
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, requestType, encryptedPayload, ETH_TOKEN, big.NewInt(0), maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to withdraw %s %s: %v\n", c.value, tokenInfo.Symbol, err)
				return
			}

			fmt.Println("Waiting for confirmation from Vela")

			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Withdrawal failed: %v\n", err)
				return
			}

			fmt.Println("Withdrawal completed successfully, funds moved from the private state to the bridge contract and ready to be claimed")
			fmt.Printf("IMPORTANT: execute 'claimpendingpayments --token %s' to receive funds in your public address\n", tokenInfo.Symbol)

		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount to withdraw (e.g., '1.5 ETH', '100' for ERC-20)")
	cmd.Flags().StringVarP(&c.receiver, "to", "", "", "The address that will receive the withdrawal amount")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (always ETH)")
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
