package cmd

import (
	"fmt"
	"log"
	"context"
	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/spf13/cobra"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type GetPublicBalanceCommand struct {
	*app.AppCommand
}

func NewGetPublicBalanceCommand(config *app.Config) *GetPublicBalanceCommand {
	return &GetPublicBalanceCommand{
		AppCommand: app.NewAppCommand(config),
	}
}

func (c *GetPublicBalanceCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getpublicbalance",
		Short: `display BASE balance of this wallet`,
		Long:  `display BASE balance of this wallet`,

		
		Run: func(cmd *cobra.Command, args []string) {
			
			if c.Config.KeySecp == nil {
				fmt.Println("Error: Secp256k1 key not found in the wallet")
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

			eth := new(big.Float).Quo(new(big.Float).SetInt(balance), big.NewFloat(1e18))
			fmt.Println(eth.Text('f', 18))
		},
	}
	return cmd
}
