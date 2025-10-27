package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:  "novaw",
	Long: `NovaWallet is a simple command line wallet for the Horizen Nova application`,
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func Execute() {
	rootCmd.AddCommand(NewGenerateKeysCommand().Command())
	rootCmd.AddCommand(NewGetAddressCommand(nil).Command())
	rootCmd.AddCommand(NewListPubKeysCommand(nil).Command())
	rootCmd.AddCommand(NewGetPublicBalanceCommand(nil).Command())
	rootCmd.AddCommand(NewRegisterUserCommand(nil, nil).Command())
	rootCmd.AddCommand(NewGetPrivateBalanceCommand(nil, nil).Command())
	rootCmd.AddCommand(NewDeployAppCommand(nil, nil).Command())
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
