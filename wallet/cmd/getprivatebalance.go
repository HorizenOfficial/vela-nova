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

const defaultScanDepth = 200
const scanBatchSize = 100

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

// findTokenBalance scans decrypted events backwards (most recent first) looking for
// an event whose tokenAddress matches the requested token. Returns the balance hex
// string, or "" if not found within the scan depth.
func findTokenBalance(
	ctx context.Context,
	subgraphClient subgraph.Client,
	teePubKey *cryptotypes.PublicKeyP521,
	privKey *cryptotypes.PrivateKeyP521,
	applicationID common.ApplicationIdType,
	seedSubTypes []string,
	tokenHex string,
	maxEvents int,
) (string, error) {
	if subgraphClient == nil {
		return "", fmt.Errorf("subgraph client not initialized")
	}
	if teePubKey == nil || privKey == nil {
		return "", fmt.Errorf("missing keys to decrypt user events")
	}

	scanned := 0
	for scanned < maxEvents {
		batchSize := scanBatchSize
		if scanned+batchSize > maxEvents {
			batchSize = maxEvents - scanned
		}

		events, err := FetchAndDecryptUserEvents(
			ctx,
			subgraphClient,
			teePubKey,
			*privKey,
			applicationID,
			seedSubTypes,
			batchSize,
			eventHasBalance,
		)
		if err != nil {
			return "", fmt.Errorf("can't retrieve events: %w", err)
		}

		// Check each event for a matching tokenAddress
		for _, eventJSON := range events {
			var m map[string]any
			if err := json.Unmarshal(eventJSON, &m); err != nil {
				continue
			}

			// Check if tokenAddress matches
			if tokenAddr, hasToken := m["tokenAddress"]; hasToken {
				if addrStr, ok := tokenAddr.(string); ok {
					if !strings.EqualFold(addrStr, tokenHex) {
						continue // different token, skip
					}
				}
			}

			// Found a matching event — read its balance
			if balVal, ok := m["balance"]; ok {
				if balStr, ok := balVal.(string); ok {
					return balStr, nil
				}
			}
		}

		scanned += len(events)

		// If we got fewer events than requested, there are no more
		if len(events) < batchSize {
			break
		}
	}

	return "", nil // not found
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

			if c.Config.KeySecp == nil {
				log.Fatal("secp256k1 key not found in the wallet (required to derive event subtypes)")
			}
			seed, seedErr := GenerateSeed(c.Config.KeySecp)
			if seedErr != nil {
				log.Fatalf("failed to generate seed: %v", seedErr)
			}
			seedSubTypes := EventSubTypesFromSeed(seed, DefaultSubtypeN)

			tokenHex := strings.ToLower(tokenInfo.Address.Hex())

			scanDepth := c.Config.PrivateBalanceScanDepth
			if scanDepth <= 0 {
				scanDepth = defaultScanDepth
			}

			balanceHex, err := findTokenBalance(ctx, c.SubgraphClient, teePubKey, c.Config.KeyP521, c.Config.ApplicationID, seedSubTypes, tokenHex, scanDepth)
			if err != nil {
				log.Fatalf("failed to find balance: %v", err)
			}

			if balanceHex == "" {
				fmt.Printf("No %s balance found in the last %d events.\n", tokenInfo.Symbol, scanDepth)
				fmt.Println("For an authoritative balance, use: requestreport --report-type balances")
				return
			}

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
