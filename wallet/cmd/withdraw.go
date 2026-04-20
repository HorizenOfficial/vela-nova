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
	return &WithdrawCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

// Exec runs the withdraw flow outside of the Cobra wrapper. Exported so test
// drivers can invoke it directly after setting flag-bound struct fields.
func (c *WithdrawCommand) Exec(ctx context.Context) error {
	if err := c.RequireApplicationID(); err != nil {
		return err
	}

	tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
	if err != nil {
		return err
	}

	amount, err := parseAssetAmount(c.Config.Tokens, tokenInfo, c.value)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
	if err != nil {
		return fmt.Errorf("invalid max fee amount: %w", err)
	}

	receiver, err := app.ValidateAndChecksumAddress(c.receiver)
	if err != nil {
		return fmt.Errorf("invalid receiver address: %w", err)
	}

	if err := c.InitChainClient(ctx); err != nil {
		return fmt.Errorf("connecting to rpc node: %w", err)
	}
	defer c.CloseClient()

	receiverAddr, err := types.HexToAddress(receiver.Hex())
	if err != nil {
		return fmt.Errorf("invalid receiver address, could not convert to byte array: %w", err)
	}
	tokenAddr, err := types.HexToAddress(tokenInfo.Address.Hex())
	if err != nil {
		return fmt.Errorf("invalid token address: %w", err)
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
		return fmt.Errorf("encrypting withdraw payload: %w", err)
	}

	// On-chain: no business asset deposited (withdrawal is from private state),
	// so tokenAddress=ETH_TOKEN and assetAmount=0.
	requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, common.Process, encryptedPayload, ETH_TOKEN, big.NewInt(0), maxFeeValue)
	if err != nil {
		return fmt.Errorf("sending request to withdraw %s %s: %w", c.value, tokenInfo.Symbol, err)
	}

	fmt.Println("Waiting for confirmation from Vela")
	if err := c.WaitForRequestCompleted(requestID, ctx); err != nil {
		return fmt.Errorf("Withdrawal failed: %w", err)
	}

	fmt.Println("Withdrawal completed successfully, funds moved from the private state to the bridge contract and ready to be claimed")
	fmt.Printf("IMPORTANT: execute 'claimpendingpayments --token %s' to receive funds in your public address\n", tokenInfo.Symbol)
	return nil
}

func (c *WithdrawCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw",
		Short: `withdraw funds from the Vela system`,
		Long:  `withdraw funds from the Vela system and send them to a receiver address`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount to withdraw (e.g., '1.5 ETH', '100' for ERC-20)")
	cmd.Flags().StringVarP(&c.receiver, "to", "", "", "The address that will receive the withdrawal amount")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (always ETH)")
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
