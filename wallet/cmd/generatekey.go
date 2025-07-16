package cmd

import (
	"fmt"

	"github.com/horizen-pes-nova/wallet/app/services"
	"github.com/spf13/cobra"
)

var generateKeyCmd = &cobra.Command{
	Use:   "generatekeys",
	Short: `generate new private keys and save them to the local folder`,
	Long:  `generate new private keys and save them to the local folder`,
	Run: func(cmd *cobra.Command, args []string) {
		keyP521, key25519 := services.GenerateKeys()
		fmt.Println(string("P521:"))
		fmt.Println(string(keyP521))
		fmt.Println(string("25519:"))
		fmt.Println(string(key25519))
	},
}
