package cmd

import (
	"fmt"
	"os"
	
	"github.com/spf13/cobra"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
)

var rootCmd = &cobra.Command{
	Use:  "novaw",
	Long: `NovaWallet is a simple command line wallet for the Horizen Nova application`,
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func Execute() {	
	config, err := app.LoadConfigFromFile(app.ConfFileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while loading configuration from file: %v\n", err)
        os.Exit(1)
	}
	rootCmd.AddCommand(NewGenerateKeysCommand().Command())
	rootCmd.AddCommand(NewGetAddressCommand(config).Command())
	rootCmd.AddCommand(NewListPubKeysCommand(config).Command())
	rootCmd.AddCommand(NewGetPublicBalanceCommand(config).Command())
	rootCmd.AddCommand(NewRegisterUserCommand(config, nil).Command())
	rootCmd.AddCommand(NewDepositCommand(config, nil).Command())
	rootCmd.AddCommand(NewPrivateTransferCommand(config, nil).Command())
	rootCmd.AddCommand(NewGetPrivateBalanceCommand(config, nil).Command())
	rootCmd.AddCommand(NewDecryptReportCommand(config, nil).Command())
	rootCmd.AddCommand(NewWithdrawCommand(config, nil).Command())
	rootCmd.AddCommand(NewRequestReportCommand(config, nil).Command())
	rootCmd.AddCommand(NewDownloadReportCommand(config, nil).Command())
	rootCmd.AddCommand(NewDeployAppCommand(config, nil, app.ConfFileName).Command())
	rootCmd.AddCommand(NewGetPendingPaymentsCommand(config, nil).Command())
	rootCmd.AddCommand(NewClaimPendingPaymentsCommand(config, nil).Command())
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
