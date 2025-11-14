package cmd

import (
	"fmt"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/spf13/cobra"
)

type GetAddressCommand struct {
	*app.AppCommand
}

func NewGetAddressCommand(config *app.Config) *GetAddressCommand {
	return &GetAddressCommand{
		AppCommand: app.NewAppCommand(config),
	}
}

func (c *GetAddressCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getaddress",
		Short: `display Ethereum public address of this wallet`,
		Long:  `display Ethereum public address of this wallet`,
		Run: func(cmd *cobra.Command, args []string) {
			if c.Config.KeySecp == nil {
				fmt.Println("Error: Secp256k1 key not found in the wallet")
				return
			}
			fmt.Println(c.Config.KeySecp.PublicKey().Address())
		},
	}
	return cmd
}
