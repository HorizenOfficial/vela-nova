package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var getaddressCmd = &cobra.Command{
	Use:   "getaddress",
	Short: `display Ethereum public address of this wallet`,
	Long:  `display Ethereum public address of this wallet`,
	Run: func(cmd *cobra.Command, args []string) {
		LoadConf()
		fmt.Println(appContext.KeySecp.PublicKey().Address())
	},
}
