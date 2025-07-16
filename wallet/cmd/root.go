package cmd

import (
	"fmt"
	"os"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/magiconair/properties"
	"github.com/spf13/cobra"
)

const confFileName = "wallet.conf"

var (
	appContext = &app.AppContext{} // shared instance
)

var rootCmd = &cobra.Command{
	Use:  "novaw",
	Long: `NovaWallet is a simple command line wallet for the Horizen Nova application`,
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	loadConf()
	rootCmd.AddCommand(generateKeyCmd)
}

func loadConf() {
	if !fileExists(confFileName) {
		panic("File conf not found. Please create it and restart.")
	} else {
		// Load properties from file
		config, err := properties.LoadFile(confFileName, properties.UTF8)
		if err != nil {
			panic(err)
		}
		keyP521 := config.MustGetString("keyP521")
		fmt.Printf("Key loaded: %s", keyP521)

		key25519 := config.MustGetString("key25519")
		fmt.Printf("Key loaded: %s", key25519)
	}

}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}
