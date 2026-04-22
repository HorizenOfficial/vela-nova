package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/spf13/cobra"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type GetPublicBalanceCommand struct {
	*app.AppCommand
	token string
}

func NewGetPublicBalanceCommand(config *app.Config) *GetPublicBalanceCommand {
	return &GetPublicBalanceCommand{
		AppCommand: app.NewAppCommand(config),
	}
}

func (c *GetPublicBalanceCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getpublicbalance",
		Short: `display public balance of this wallet`,
		Long:  `display public balance of this wallet (ETH or ERC-20 token)`,

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

			if tokenInfo.Address != ETH_TOKEN {
				// TODO: implement ERC-20 balanceOf query
				fmt.Printf("Error: ERC-20 public balance query not yet implemented for %s\n", tokenInfo.Symbol)
				return
			}

			address := c.Config.KeySecp.PublicKey().Address()
			rpcURL := c.Config.RpcUrl

			client, err := ethclient.Dial(rpcURL)
			if err != nil {
				log.Fatalf("Failed to connect to RPC: %v", err)
			}
			defer client.Close()

			account := common.HexToAddress(address)
			balance, err := client.BalanceAt(context.Background(), account, nil)
			if err != nil {
				log.Fatalf("Failed to get balance: %v", err)
			}

			fmt.Println(c.Config.Tokens.FormatAmount(balance, tokenInfo))
		},
	}
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
