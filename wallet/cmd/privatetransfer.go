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

type PrivateTransferCommand struct {
	*app.ChainCommand
	receiver    string
	value       string
	maxFeeValue string
	invoiceID   string
	token       string
}

func NewPrivateTransferCommand(config *app.Config, blockchainClient blockchain.Client) *PrivateTransferCommand {
	cmd := &PrivateTransferCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}

	return cmd
}

// Exec runs the privatetransfer flow outside of the Cobra wrapper. Exported
// so test drivers can invoke it directly after setting flag-bound struct
// fields.
func (c *PrivateTransferCommand) Exec(ctx context.Context) error {
	if err := c.RequireApplicationID(); err != nil {
		return err
	}

	tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
	if err != nil {
		return err
	}

	to, err := app.ValidateAndChecksumAddress(c.receiver)
	if err != nil {
		return fmt.Errorf("invalid receiver address: %w", err)
	}

	amount, err := parseAssetAmount(c.Config.Tokens, tokenInfo, c.value)
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

	toAddr, err := types.HexToAddress(to.Hex())
	if err != nil {
		return fmt.Errorf("invalid receiver address, could not convert to byte array: %w", err)
	}

	if len(c.invoiceID) > runtimeapp.MaxInvoiceIDLength {
		return fmt.Errorf("invoice_id exceeds maximum length of %d characters", runtimeapp.MaxInvoiceIDLength)
	}

	tokenAddr, err := types.HexToAddress(tokenInfo.Address.Hex())
	if err != nil {
		return fmt.Errorf("invalid token address: %w", err)
	}

	payload := runtimeapp.PayloadInstructions{
		Type: "transfer",
		Transfer: &runtimeapp.TransferInstruction{
			To:           toAddr,
			TokenAddress: tokenAddr,
			Amount:       new(types.Uint256).SetBytes(amount.Bytes()),
			InvoiceID:    c.invoiceID,
		},
	}
	encryptedPayload, err := c.EncryptPayload(&payload, ctx)
	if err != nil {
		return fmt.Errorf("encrypting private transfer payload: %w", err)
	}

	// On-chain: no business asset deposited (it's a private-state transfer),
	// so tokenAddress=ETH_TOKEN and assetAmount=0.
	requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, common.Process, encryptedPayload, ETH_TOKEN, big.NewInt(0), maxFeeValue)
	if err != nil {
		return fmt.Errorf("sending request to transfer amount %s to %s: %w", c.value, to, err)
	}

	fmt.Printf("Waiting for confirmation from Vela for requestID: %s\n", requestID)
	if err := c.WaitForRequestCompleted(requestID, ctx); err != nil {
		return fmt.Errorf("Private transfer failed: %w", err)
	}
	fmt.Println("Private transfer completed successfully")
	return nil
}

func (c *PrivateTransferCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "privatetransfer",
		Short: `submits a private transfer request to a receiver address`,
		Long:  `submits a private transfer request to a receiver address`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount to transfer (e.g., '1.5 ETH', '100' for ERC-20)")
	cmd.Flags().StringVarP(&c.receiver, "to", "t", "", "The receiver address of the private transfer")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (always ETH)")
	cmd.Flags().StringVarP(&c.invoiceID, "invoice-id", "i", "", "Optional invoice ID to include in the transfer event")
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
