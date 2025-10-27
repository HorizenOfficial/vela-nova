package cmd

import (
	"fmt"
	"context"
	"log"
	"os"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/spf13/cobra"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/crypto"

)

type DecryptReportCommand struct {
	*app.AppCommand
	customClient blockchain.Client
}

func NewDecryptReportCommand(config *app.Config, customClient blockchain.Client) *DecryptReportCommand {
	return &DecryptReportCommand{
		AppCommand: app.NewAppCommand(config),
		customClient: customClient,
	}
}

func (c *DecryptReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decryptreport",
		Short: `decrypt a deanonymization report specifying file that contains it`,
		Long: `decrypt a deanonymization report specifying file that contains it`,
		Run: func(cmd *cobra.Command, args []string) {
			//check args
			if(len(args) < 1) {
				log.Fatalf("file path parameter is required")
			}
			//get file path
			file := args[0]
			readBytes, err := os.ReadFile(file)
			if err != nil {
				log.Fatalf("error reading file %s: %v", file, err)
			}
			
			//init blockchain client to get decryption public key
			blockchainClient := c.customClient
			if blockchainClient == nil {
				//init blockchain client
				blockchainClient = blockchain.NewBlockChainClient(c.Config.ProcessorEndpointAddress, c.Config.TeeAuthenticatorAddress, c.Config.RpcUrl, &c.Config.KeySecp)
				err := blockchainClient.Connect(context.Background())
				if err != nil {
					log.Fatalf("Error connecting to rpc node: %v", err)
					return
				}			
			}
			//get public key
			publicKey, err := blockchainClient.GetTeePublicKey(context.Background())
			if err != nil {
				log.Fatalf("error retrieving public key to encrypt: %v", err)
			}
			// decrypt
			decrypted, err := crypto.Decrypt(publicKey, &c.Config.KeyP521, []byte(readBytes))
			if err != nil {
				log.Fatalf("error decrypting report payload: %v", err)
			}
			//transform to string and print
			strDecrypted := string(decrypted)
			fmt.Println("Decrypted report:")
			fmt.Println(strDecrypted)
		},
	}
	return cmd
}

