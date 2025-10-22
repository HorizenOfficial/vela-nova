package cmd

import (
	"fmt"
	"context"
	"encoding/json"
	"log"
	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"
	"github.com/horizen-pes/pkg/blockchain"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
)

var NOVA_APPLICATION_ID = *big.NewInt(1)
const BLOCK_BATCH_SIZE = 100
const BALANCE_JSON_KEY = "balance"

type GetPrivateBalanceCommand struct {
	*app.AppCommand
	customClient blockchain.Client
}

func NewGetPrivateBalanceCommand(config *app.Config, customClient blockchain.Client) *GetPrivateBalanceCommand {
	return &GetPrivateBalanceCommand{
		AppCommand: app.NewAppCommand(config),
		customClient: customClient,
	}
}

func eventFilter(b []byte) bool {
	var m map[string]any
	err := json.Unmarshal(b, &m)
	return err == nil && m[BALANCE_JSON_KEY] != nil
}

func (c *GetPrivateBalanceCommand) FindEvent(blockchainClient blockchain.Client, privKey cryptotypes.PrivateKeyP521, latestBlock uint64) ([]byte, error) {
	// define search range
	fromBlock := latestBlock
	var toBlock uint64 = 0
	if fromBlock > BLOCK_BATCH_SIZE {
		toBlock = fromBlock - BLOCK_BATCH_SIZE
	}
	//start loop
	for true {
		events, err := blockchainClient.GetUserEvents(
			context.Background(), 
			privKey, 
			NOVA_APPLICATION_ID, 
			fromBlock, 
			toBlock, 
			eventFilter, 
			true,
		);
		if err != nil {
			return nil, fmt.Errorf("can't retrieve events: %w", err)//stop
		}
		//event found, return the first
		if len(events) > 0 {
			return events[0], nil
		}
		//event not found, check if block are finished
		if toBlock == 0 {
			return nil, fmt.Errorf("can't find events at any block") //stop
		}
		//redefine search range
		fromBlock = 0
		if toBlock > 0 {
			fromBlock = toBlock - 1
		}
		toBlock = 0
		if fromBlock > BLOCK_BATCH_SIZE {
			toBlock = fromBlock - BLOCK_BATCH_SIZE
		}
	}
	return nil, nil
}

func (c *GetPrivateBalanceCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getprivatebalance",
		Short: `get private balance associated to the wallet address`,
		Long:  `get private balance associated to the wallet address`,
		Run: func(cmd *cobra.Command, args []string) {
			//init blockchain client
			blockchainClient := blockchain.NewBlockChainClient(
				c.Config.ProcessorEndpointAddress,
				c.Config.TeeAuthenticatorAddress,
				c.Config.RpcUrl,
				nil,
			)
			//get last block number
			client, err := ethclient.Dial(c.Config.RpcUrl)
			if err != nil {
				log.Fatalf("Failed to connect to RPC: %v", err)
			}
			defer client.Close()
			latestBlock, err := client.BlockNumber(context.Background())
			if err != nil {
				log.Fatalf("failed to get latest block: %v", err)
			}

			//decryption key
			privKey := cryptotypes.PrivateKeyP521{PrivateKey: c.Config.KeyP521.PrivateKey}

			clientToUse := c.customClient
			if clientToUse == nil {
				//use the real client
				clientToUse = blockchainClient
			}

			//find event
			event, err := c.FindEvent(clientToUse, privKey, latestBlock)
			if err != nil {
				log.Fatalf("failed to find event: %v", err)
			}

			//get json from event
			var jsonData map[string]interface{} 	
			err = json.Unmarshal(event, &jsonData) 
			if err != nil {
				log.Fatalf("failed to convert event to json: %v", err)
			}
			//print balance
			fmt.Println(jsonData["balance"])
		},
	}
	return cmd
}
