package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/subgraph"
	"github.com/spf13/cobra"
)

type GetPrivateBalanceCommand struct {
	*app.ChainCommand
}

func NewGetPrivateBalanceCommand(config *app.Config, customClient blockchain.Client) *GetPrivateBalanceCommand {
	chainCmd := app.NewChainCommand(config, customClient)
	return &GetPrivateBalanceCommand{
		ChainCommand: chainCmd,
	}
}

func EventFilter(b []byte) bool {
	var m map[string]any
	err := json.Unmarshal(b, &m)
	return err == nil && m[BALANCE_JSON_KEY] != nil
}

func FindEvent(ctx context.Context, subgraphClient subgraph.Client, teePubKey *cryptotypes.PublicKeyP521, privKey *cryptotypes.PrivateKeyP521) ([]byte, error) {
	if subgraphClient == nil {
		return nil, fmt.Errorf("subgraph client not initialized")
	}
	if teePubKey == nil || privKey == nil {
		return nil, fmt.Errorf("missing keys to decrypt user events")
	}

	events, err := subgraph.FetchAndDecryptUserEvents(
		ctx,
		subgraphClient,
		teePubKey,
		*privKey,
		NOVA_APPLICATION_ID,
		"",
		1,
		EventFilter,
	)
	if err != nil {
		return nil, fmt.Errorf("can't retrieve events: %w", err)
	}
	if len(events) == 0 {
		return []byte(`{"` + BALANCE_JSON_KEY + `": "0x0"}`), nil
	}
	return events[0], nil
}

func (c *GetPrivateBalanceCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getprivatebalance",
		Short: `get private balance associated to the wallet address`,
		Long:  `get private balance associated to the wallet address`,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			if cmd != nil && cmd.Context() != nil {
				ctx = cmd.Context()
			}
			blockchainClient := c.BlockchainClient
			if blockchainClient == nil {
				if err := c.InitChainClient(ctx); err != nil {
					log.Fatalf("Error connecting to rpc node: %v", err)
					return
				}
				blockchainClient = c.BlockchainClient
			}
			defer blockchainClient.Close()

			if c.SubgraphClient == nil {
				log.Fatal("subgraph client not initialized (missing SubgraphURL)")
			}

			teePubKey, err := blockchainClient.GetTeePublicKey(ctx)
			if err != nil {
				log.Fatalf("failed to get TEE public key: %v", err)
			}

			//find event
			event, err := FindEvent(ctx, c.SubgraphClient, teePubKey, c.Config.KeyP521)
			if err != nil {
				log.Fatalf("failed to find event: %v", err)
			}

			//get json from event
			var jsonData struct {
				Balance *common.Big
			}
			err = json.Unmarshal(event, &jsonData)
			if err != nil {
				log.Fatalf("failed to convert event to json: %v", err)
			}
			//print balance
			fmt.Println(app.WeiToEtherStr(jsonData.Balance.ToInt()))
		},
	}
	return cmd
}
