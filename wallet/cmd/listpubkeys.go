package cmd

import (
	"fmt"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/spf13/cobra"
)

type ListPubKeysCommand struct {
	*app.AppCommand
}

func NewListPubKeysCommand(config *app.Config) *ListPubKeysCommand {
	return &ListPubKeysCommand{
		AppCommand: app.NewAppCommand(config),
	}
}

func (c *ListPubKeysCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "listpubkeys",
		Short: `list public keys loaded from local configuration`,
		Long:  `list public keys loaded from local configuration`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(string("P521: "))
			if c.Config.KeyP521 == nil {
				fmt.Println("not set")
			} else {
				fmt.Println(crypto.ExportPublicKeyP521ToHex(c.Config.KeyP521.PublicKey()))
			}
			fmt.Println(string("secp: "))
			if c.Config.KeySecp == nil {
				fmt.Println("not set")
				return
			}
			fmt.Println(crypto.ExportPublicKeySecp256k1ToHex(c.Config.KeySecp.PublicKey()))
		},
	}
	return cmd
}
