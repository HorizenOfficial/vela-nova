package cmd

import (
	"fmt"
	"context"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
	"github.com/horizen-pes/pkg/blockchain"
)

type RegisterUserCommand struct {
	*app.AppCommand
	useMockClient bool
}

func NewRegisterUserCommand(config *app.Config, useMockClient bool) *RegisterUserCommand {
	return &RegisterUserCommand{
		AppCommand: app.NewAppCommand(config),
		useMockClient: useMockClient,
	}
}

func (c *RegisterUserCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registeruser",
		Short: `register the association [address, encryption key (P521)] of the wallet into the KeyRegistry smart contract`,
		Long:  `register the association [address, encryption key (P521)] of the wallet into the KeyRegistry smart contract`,
		Run: func(cmd *cobra.Command, args []string) {
			//read config
			keyP521 := c.Config.KeyP521.PublicKey()

			if !c.useMockClient {
				//create blockchain client and call register PK method
				rpcUrl := c.Config.RpcUrl
				keyRegistryAddress := common.HexToAddress(c.Config.KeyRegistryAddress)

				blockchainClient := blockchain.NewBlockChainClient(keyRegistryAddress, keyRegistryAddress, rpcUrl, c.Config.KeySecp.PrivateKey)
				blockchainClient.Connect(context.Background())
				err := blockchainClient.RegisterPK(context.Background(), keyP521.Bytes())
				if err != nil {
					fmt.Println("Error registering public key:", err)
					blockchainClient.Close()
					return
				}
				blockchainClient.Close()
			} else {
				mockBlockchainClient := blockchain.NewMockClient()
				mockBlockchainClient.RegisterPublicKey(context.Background(), c.Config.KeySecp.PublicKey().Address(), keyP521.Bytes())
			}
			fmt.Println("Public key registered successfully")
		},
	}
	return cmd
}
