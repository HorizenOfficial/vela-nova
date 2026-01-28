package cmd

import (
	"fmt"
	"math/big"

	runtimeapp "github.com/horizen-pes-nova/payment-app/app"
	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)

type WithdrawCommand struct {
	*app.ChainCommand
	value       string
	maxFeeValue string
	receiver    string
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
		Short: `withdraw funds from the PES system`,
		Long:  `withdraw funds from the PES system and send them to a receiver address`,
		Run: func(cmd *cobra.Command, args []string) {

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

			receiver, err := app.ValidateAndChecksumAddress(c.receiver)
			if err != nil {
				fmt.Printf("Error: invalid receiver address: %v\n", err)
				return
			}

			ctx := cmd.Context()
			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(ctx); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return
				}
			}
			defer c.CloseClient()

			payload := runtimeapp.PayloadInstructions{
				Type:     "withdraw",
				Withdraw: &runtimeapp.WithdrawInstruction{To: receiver, Amount: amount},
			}
			encryptedPayload, err := c.EncryptPayload(&payload, ctx)
			if err != nil {
				fmt.Printf("Error encrypting withdraw payload: %v\n", err)
				return
			}

			requestType := common.Process
			requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, encryptedPayload, big.NewInt(0), maxFeeValue)
			if err != nil {
				fmt.Printf("Error sending request to withdraw amount %s: %v\n", c.value, err)
				return
			}

			fmt.Println("Waiting for confirmation from PES")

			err = c.WaitForRequestCompleted(requestID, ctx)
			if err != nil {
				fmt.Printf("Withdrawal failed: %v\n", err)
				return
			}

			fmt.Println("Withdrawal completed successfully")

		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount of Ether to withdraw (e.g., 1.5 ETH). It can be specified in ETH, Wei or GWei. Eg --amount \"147777 Wei\"")
	cmd.Flags().StringVarP(&c.receiver, "to", "", "", "The address that will receive the withdrawal amount")
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	return cmd
}
