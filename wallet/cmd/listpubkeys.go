package cmd

import (
	"fmt"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/crypto"
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
			fmt.Println(string("P521:"))
			fmt.Println(crypto.ExportPublicKeyP521ToHex(c.Config.KeyP521.PublicKey()))
			fmt.Println(string("secp:"))
			fmt.Println(crypto.ExportPublicKeySecp256k1ToHex(c.Config.KeySecp.PublicKey()))
		},
	}
	return cmd
}
