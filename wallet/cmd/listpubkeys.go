package cmd

import (
	"fmt"

	"github.com/horizen-pes/pkg/crypto"
	"github.com/spf13/cobra"
)

var listpubKeysCmd = &cobra.Command{
	Use:   "listpubkeys",
	Short: `list public keys loaded from local configuration`,
	Long:  `list public keys loaded from local configuration`,
	Run: func(cmd *cobra.Command, args []string) {
		LoadConf()
		fmt.Println(string("P521:"))
		fmt.Println(crypto.ExportPublicKeyP521ToHex(appContext.KeyP521.PublicKey()))
		fmt.Println(string("secp:"))
		fmt.Println(crypto.ExportPublicKeySecp256k1ToHex(appContext.KeySecp.PublicKey()))
	},
}
