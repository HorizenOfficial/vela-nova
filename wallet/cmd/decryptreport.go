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
	*app.ChainCommand
	filePath string
}


func NewDecryptReportCommand(config *app.Config, blockchainClient blockchain.Client) *DecryptReportCommand {
	cmd := &DecryptReportCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
	return cmd
}

func (c *DecryptReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decryptreport",
		Short: `decrypt a deanonymization report specifying file that contains it`,
		Long: `decrypt a deanonymization report specifying file that contains it`,
		Run: func(cmd *cobra.Command, args []string) {
			readBytes, err := os.ReadFile(filePath)
			if err != nil {
				log.Fatalf("error reading file %s: %v", filePath, err)
			}
			
			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return 
				}
			} 
			defer c.CloseClient()
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
	cmd.Flags().StringVarP(&c.filePath, "path", "p", "", "The path of the file to decrypt (e.g., /path/to/file.txt).")
	return cmd
}

