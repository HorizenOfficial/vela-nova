package cmd

import (
	"fmt"
	"context"
	"log"
	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/crypto"

)

type PrivateTransferCommand struct {
	*app.AppCommand
	customClient blockchain.Client
}

func NewPrivateTransferCommand(config *app.Config, customClient blockchain.Client) *PrivateTransferCommand {
	return &PrivateTransferCommand{
		AppCommand: app.NewAppCommand(config),
		customClient: customClient,
	}
}

func (c *PrivateTransferCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "privatetransfer",
		Short: `submits a private transfer request specifying receiver address and amount as parameters`,
		Long: `submits a private transfer request specifying receiver address and amount as parameters`,
		Run: func(cmd *cobra.Command, args []string) {
			//check args
			if(len(args) < 2) {
				log.Fatalf("receiver address and amount parameters are required")
			}
			//get receiver
			toAddress := args[0]
			if !ethCommon.IsHexAddress(toAddress) {
				log.Fatalf("first argument %s is not a valid hex address", args[0])
			}

			//get amount
    		f, _, err := big.ParseFloat(args[1], 10, 0, big.ToNearestEven)
			if err != nil {
				log.Fatalf("second argument %s is not a valid amount: %v", args[1], err)
			}
			weiFloat := new(big.Float).Mul(f, big.NewFloat(1e18))
			weiInt := new(big.Int)
			weiFloat.Int(weiInt)
			amount := weiInt.String()
			
			//init blockchain client
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

			//build body with type transfer
			jsonBody := `{"type":"trasfer", "transfer": {"amount":"` + amount + `, "to": "` + toAddress +`"}}`
			//get public key
			publicKey, err := blockchainClient.GetTeePublicKey(context.Background())
			if err != nil {
				log.Fatalf("error retrieving public key to encrypt: %v", err)
			}
			// encrypt
			payload, err := crypto.Encrypt(&c.Config.KeyP521, publicKey, []byte(jsonBody))
			if err != nil {
				log.Fatalf("error encrypting private transfer payload: %v", err)
			}
			//submit request
			requestId, _, err := blockchainClient.SubmitRequest(context.Background(), PROTOCOL_VERSION, &NOVA_APPLICATION_ID, common.Process, payload, big.NewInt(0))
			if err != nil {
				log.Fatalf("error submitting private transfer request: %v", err)
			}
			fmt.Printf("Private transfer request submitted with request id: %s\n", requestId)			
		},
	}
	return cmd
}

