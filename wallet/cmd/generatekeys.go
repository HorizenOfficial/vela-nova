package cmd

import (
	"fmt"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/app/services"
	"github.com/spf13/cobra"
)

type GenerateKeysCommand struct {
	*app.AppCommand
}

func NewGenerateKeysCommand() *GenerateKeysCommand {
	return &GenerateKeysCommand{
		AppCommand: app.NewAppCommandNoConfig(),
	}
}

func (c *GenerateKeysCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generatekeys",
		Short: `generate new private keys and print them on the terminal`,
		Long:  `generate new private keys and print them on the terminal`,
		Run: func(cmd *cobra.Command, args []string) {
			keyP521, keySecp := services.GenerateKeys()
			fmt.Println(string("P521:"))
			fmt.Println(string(keyP521))
			fmt.Println(string("secp:"))
			fmt.Println(string(keySecp))
		},
	}
	return cmd
}
