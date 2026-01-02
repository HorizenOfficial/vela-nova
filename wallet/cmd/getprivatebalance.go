package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/spf13/cobra"
)

type GetPrivateBalanceCommand struct {
	*app.AppCommand
	customClient blockchain.Client
}

func NewGetPrivateBalanceCommand(config *app.Config, customClient blockchain.Client) *GetPrivateBalanceCommand {
	return &GetPrivateBalanceCommand{
		AppCommand:   app.NewAppCommand(config),
		customClient: customClient,
	}
}

func EventFilter(b []byte) bool {
	var m map[string]any
	err := json.Unmarshal(b, &m)
	return err == nil && m[BALANCE_JSON_KEY] != nil
}

func FindEvent(blockchainClient blockchain.Client, privKey *cryptotypes.PrivateKeyP521, latestBlock uint64) ([]byte, error) {
	// define search range
	fromBlock := latestBlock
	var toBlock uint64 = 0
	if fromBlock > BLOCK_BATCH_SIZE {
		toBlock = fromBlock - BLOCK_BATCH_SIZE
	}
	//start loop
	for {
		events, err := blockchainClient.GetUserEvents(
			context.Background(),
			*privKey,
			NOVA_APPLICATION_ID,
			fromBlock,
			toBlock,
			"",
			EventFilter,
			true,
		)
		if err != nil {
			return nil, fmt.Errorf("can't retrieve events: %w", err) //stop
		}
		//event found, return the first
		if len(events) > 0 {
			return events[0], nil
		}
		//event not found, check if block are finished
		if toBlock == 0 {
			//if finished, balance 0
			return []byte(`{"` + BALANCE_JSON_KEY + `": 0}`), nil
		}
		//redefine search range
		fromBlock = toBlock - 1
		toBlock = 0
		if fromBlock > BLOCK_BATCH_SIZE {
			toBlock = fromBlock - BLOCK_BATCH_SIZE
		}
	}
}

func (c *GetPrivateBalanceCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getprivatebalance",
		Short: `get private balance associated to the wallet address`,
		Long:  `get private balance associated to the wallet address`,
		Run: func(cmd *cobra.Command, args []string) {
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

			blockchainClient := c.customClient
			if blockchainClient == nil {
				//init blockchain client
				blockchainClient = blockchain.NewBlockChainClient(*c.Config.ProcessorEndpointAddress, *c.Config.TeeAuthenticatorAddress, c.Config.RpcUrl, c.Config.KeySecp)
				err := blockchainClient.Connect(context.Background())
				if err != nil {
					log.Fatalf("Error connecting to rpc node: %v", err)
					return
				}
			}
			defer blockchainClient.Close()

			//find event
			event, err := FindEvent(blockchainClient, c.Config.KeyP521, latestBlock)
			if err != nil {
				log.Fatalf("failed to find event: %v", err)
			}

			//get json from event
			var jsonData struct {
				Balance *big.Int
			}
			err = json.Unmarshal(event, &jsonData)
			if err != nil {
				log.Fatalf("failed to convert event to json: %v", err)
			}
			//print balance
			eth := new(big.Float).Quo(new(big.Float).SetInt(jsonData.Balance), big.NewFloat(1e18))
			fmt.Println(eth.Text('f', 18))
		},
	}
	return cmd
}
