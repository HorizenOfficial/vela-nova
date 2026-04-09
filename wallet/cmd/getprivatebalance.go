package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela-common-go/subgraph"
	"github.com/spf13/cobra"
)

type GetPrivateBalanceCommand struct {
	*app.ChainCommand
	token string
}

func NewGetPrivateBalanceCommand(config *app.Config, customClient blockchain.Client) *GetPrivateBalanceCommand {
	chainCmd := app.NewChainCommand(config, customClient)
	return &GetPrivateBalanceCommand{
		ChainCommand: chainCmd,
	}
}

// eventHasBalance returns true if the decrypted event JSON contains a "balance" key.
func eventHasBalance(b []byte) bool {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	return m["balance"] != nil
}

// findLatestEvent fetches the most recent decrypted event that contains balance information.
func findLatestEvent(ctx context.Context, subgraphClient subgraph.Client, teePubKey *cryptotypes.PublicKeyP521, privKey *cryptotypes.PrivateKeyP521, applicationID common.ApplicationIdType) ([]byte, error) {
	if subgraphClient == nil {
		return nil, fmt.Errorf("subgraph client not initialized")
	}
	if teePubKey == nil || privKey == nil {
		return nil, fmt.Errorf("missing keys to decrypt user events")
	}

	events, err := FetchAndDecryptUserEvents(
		ctx,
		subgraphClient,
		teePubKey,
		*privKey,
		applicationID,
		"",
		1,
		eventHasBalance,
	)
	if err != nil {
		return nil, fmt.Errorf("can't retrieve events: %w", err)
	}
	if len(events) == 0 {
		return nil, nil // no events found
	}
	return events[0], nil
}

// extractBalance extracts the balance for the given token from a decrypted event.
// It handles the event format where "balance" is the per-token balance and
// "tokenAddress" identifies which token the balance refers to.
func extractBalance(eventJSON []byte, tokenHex string) string {
	var m map[string]any
	if err := json.Unmarshal(eventJSON, &m); err != nil {
		return "0x0"
	}

	// Check if the event has a tokenAddress field that matches
	if tokenAddr, hasToken := m["tokenAddress"]; hasToken {
		if addrStr, ok := tokenAddr.(string); ok {
			if !strings.EqualFold(addrStr, tokenHex) {
				// Event is for a different token
				return "0x0"
			}
		}
	}

	// Read the "balance" field
	if balVal, ok := m["balance"]; ok {
		if balStr, ok := balVal.(string); ok {
			return balStr
		}
	}

	return "0x0"
}

func (c *GetPrivateBalanceCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getprivatebalance",
		Short: `get private balance associated to the wallet address`,
		Long:  `get private balance associated to the wallet address`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.RequireApplicationID(); err != nil {
				log.Fatalf("Error: %v", err)
			}

			// Resolve token
			tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
			if err != nil {
				log.Fatalf("Error: %v", err)
			}

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

			event, err := findLatestEvent(ctx, c.SubgraphClient, teePubKey, c.Config.KeyP521, c.Config.ApplicationID)
			if err != nil {
				log.Fatalf("failed to find event: %v", err)
			}

			tokenHex := strings.ToLower(tokenInfo.Address.Hex())

			if event == nil {
				fmt.Printf("0 %s\n", tokenInfo.Symbol)
				return
			}

			balanceHex := extractBalance(event, tokenHex)

			var balance common.Big
			if err := json.Unmarshal([]byte(`"`+balanceHex+`"`), &balance); err != nil {
				log.Fatalf("failed to parse balance: %v", err)
			}

			fmt.Println(c.Config.Tokens.FormatAmount(balance.ToInt(), tokenInfo))
		},
	}
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
