package cmd

import (
	"fmt"
	"context"
	"encoding/json"
	"log"
	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"
	"github.com/horizen-pes/pkg/blockchain"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
)

const BLOCK_BATCH_SIZE = 100

type GetPrivateBalanceCommand struct {
	*app.AppCommand
	knownLastEvent []byte
}

func NewGetPrivateBalanceCommand(config *app.Config, knownLastEvent []byte) *GetPrivateBalanceCommand {
	return &GetPrivateBalanceCommand{
		AppCommand: app.NewAppCommand(config),
		knownLastEvent: knownLastEvent,
	}
}

func (c *GetPrivateBalanceCommand) FindEvent(blockchainClient blockchain.Client, privKey cryptotypes.PrivateKeyP521, applicationId big.Int, latestBlock uint64) ([]byte, error) {
	// define search range
	fromBlock := latestBlock
	var toBlock uint64 = 0
	if fromBlock > BLOCK_BATCH_SIZE {
		toBlock = fromBlock - BLOCK_BATCH_SIZE
	}
	//start loop
	for true {
		fmt.Printf("Searching from block %d to %d\n", fromBlock, toBlock)
		events, err := blockchainClient.GetUserEvents(
			context.Background(), 
			privKey, 
			applicationId, 
			fromBlock, 
			toBlock, 
			nil, 
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
		Use:   "getprivatebalance $applicationId",
		Short: `get private balance from the last event from ProcessorEndpoint smart contract that the user can decrypt`,
		Long:  `get private balance from the last event from ProcessorEndpoint smart contract that the user can decrypt`,
		Run: func(cmd *cobra.Command, args []string) {

			//read first parameter as applicationId
			if len(args) < 1 {
				log.Fatalf("applicationId parameter is required")
			}
			//parse to big int
			applicationId := new(big.Int)
			applicationId, ok := applicationId.SetString(args[0], 10)
			if !ok {
				log.Fatalf("Failed to parse to uint applicationId: %s", args[0])
			}
			//init blockchain client
			blockchainClient := blockchain.NewBlockChainClient(
				common.HexToAddress(c.Config.ProcessorEndpointAddress),
				common.HexToAddress(c.Config.TeeAuthenticatorAddress),
				c.Config.RpcUrl,
				nil,
			)
			//get last block number
			client, err := ethclient.Dial(c.Config.RpcUrl)
			if err != nil {
				log.Fatalf("Failed to connect to RPC: %v", err)
			}
			defer client.Close()

			//get last block
			latestBlock, err := client.BlockNumber(context.Background())
			if err != nil {
				log.Fatalf("failed to get latest block: %v", err)
			}
			//decryption key
			privKey := cryptotypes.PrivateKeyP521{PrivateKey: c.Config.KeyP521.PrivateKey}

			var event []byte
			if c.knownLastEvent != nil {
				//use the given event 
				event = c.knownLastEvent
			} else { 
				//find event
				event, err = c.FindEvent(blockchainClient, privKey, *applicationId, latestBlock)
				if err != nil {
					log.Fatalf("failed to get event: %v", err)
				}
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
