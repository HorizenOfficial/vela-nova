package cmd

import (
	"context"
	"fmt"
	"log"
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
}

func NewPrivateTransferCommand(config *app.Config, blockchainClient blockchain.Client) *PrivateTransferCommand {
	cmd := &PrivateTransferCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}

	return cmd
}

func (c *PrivateTransferCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "privatetransfer",
		Short: `submits a private transfer request to a receiver address`,
		Long:  `submits a private transfer request to a receiver address`,
		Run: func(cmd *cobra.Command, args []string) {
			//get receiver
			to, err := app.ValidateAndChecksumAddress(c.receiver)
			if err != nil {
				log.Fatalf("Error: invalid receiver: %s\n", c.receiver)
			}

			//get amount
			amount, err := app.ParseEtherValue(c.value)
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
				//create blockchain client
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return

				}
			}
			defer c.CloseClient()

			toAddr, err := types.HexToAddress(to.Hex())
			if err != nil {
				log.Fatalf("Error: invalid receiver: %s\n", c.receiver)
			}

			if len(c.invoiceID) > runtimeapp.MaxInvoiceIDLength {
				fmt.Printf("Error: invoice_id exceeds maximum length of %d characters\n", runtimeapp.MaxInvoiceIDLength)
				return
			}

			//build payload with type transfer
			payload := runtimeapp.PayloadInstructions{
				Type:     "transfer",
				Transfer: &runtimeapp.TransferInstruction{To: toAddr, Amount: new(types.Uint256).SetBytes(amount.Bytes()), InvoiceID: c.invoiceID},
			}
			encryptedPayload, err := c.EncryptPayload(&payload, ctx)
			if err != nil {
				log.Fatalf("Error encrypting private transfer payload: %v", err)
			}

			//submit request
			requestType := common.Process
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, encryptedPayload, big.NewInt(0), maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to transfer amount %s to %s: %v", c.value, to, err)
				return
			}
			fmt.Printf("Waiting for confirmation from Vela for requestID: %s\n", requestID)
			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Private transfer failed: %v\n", err)
				return
			}

			fmt.Println("Private transfer completed successfully")
		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount of Ether to process (e.g., 1.5 ETH). It can be specified in ETH, Wei or GWei. Eg --amount 147777 Wei")
	cmd.Flags().StringVarP(&c.receiver, "to", "t", "", "The receiver address of the private transfer. Eg --to 0xabc123...")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	cmd.Flags().StringVarP(&c.invoiceID, "invoice-id", "i", "", "Optional invoice ID to include in the transfer event")
	return cmd
}
