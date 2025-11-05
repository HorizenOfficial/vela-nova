package cmd

import (
	"fmt"
	"context"
	"encoding/base64"
	"encoding/json"
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

type Report struct {
	ApplicationId  string `json:"applicationId"`
	ReportId       string `json:"reportId"`
	EncryptedReport string `json:"encryptedReport"`
}

type DecryptedReport struct {
	ApplicationId  string `json:"applicationId"`
	RequestId       string `json:"requestId"`
	ReportDataBytes string `json:"reportDataBytes"`
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
			readJson, err := os.ReadFile(c.filePath)
			if err != nil {
				log.Fatalf("error reading file %s: %v", c.filePath, err)
			}
			var er Report
			err = json.Unmarshal(readJson, &er)
			if err != nil {
				log.Fatalf("error unmarshalling json from file %s: %v", c.filePath, err)
			}
			//get bytes from base64 string
			readBytes, err := base64.StdEncoding.DecodeString(er.EncryptedReport)
			if err != nil {
				log.Fatalf("error decoding base64 string from file %s: %v", c.filePath, err)
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
			publicKey, err := c.BlockchainClient.GetTeePublicKey(context.Background())
			if err != nil {
				log.Fatalf("error retrieving public key to encrypt: %v", err)
			}
			// decrypt
			decrypted, err := crypto.Decrypt(publicKey, c.Config.KeyP521, []byte(readBytes))
			if err != nil {
				log.Fatalf("error decrypting report payload: %v", err)
			}
			//unmarshall decrypted as json
			strDecrypted := string(decrypted)
			var decryptedReport DecryptedReport
			err = json.Unmarshal([]byte(strDecrypted), &decryptedReport)
			if err != nil {
				log.Fatalf("error unmarshalling json from decrypted report: %v", err)
			}
			//take report data and decode from base64
			finalJson, err := base64.StdEncoding.DecodeString(decryptedReport.ReportDataBytes)
			if err != nil {
				log.Fatalf("error decoding base64 string from decrypted report: %v", err)
			}

			fmt.Println("Decrypted report:")
			fmt.Printf("Application Id: %s\n", er.ApplicationId)
			fmt.Printf("Report Id: %s\n", er.ReportId)
			fmt.Printf("ReportData: %s\n", string(finalJson))
		},
	}
	cmd.Flags().StringVarP(&c.filePath, "path", "p", "", "The path of the file to decrypt (e.g., /path/to/file.txt).")
	return cmd
}

