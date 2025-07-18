package cmd

import (
	"fmt"

	"github.com/horizen-pes-nova/wallet/app/services"
	"github.com/spf13/cobra"
)

var generateKeyCmd = &cobra.Command{
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
