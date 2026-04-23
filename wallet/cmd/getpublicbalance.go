package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/spf13/cobra"

	"github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// BalanceClient is the subset of a chain client needed to read public balances.
// BalanceAt serves the ETH path; bind.ContractBackend serves the ERC-20 path
// (for calling balanceOf via the generated contract bindings). Both
// *ethclient.Client (production) and simulated.Client (tests) satisfy it.
type BalanceClient interface {
	bind.ContractBackend
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

type GetPublicBalanceCommand struct {
	*app.AppCommand
	token string

	// Client is an optional injected chain client. When nil (the normal CLI
	// path) Exec dials Config.RpcUrl and closes the result. When set (tests,
	// or callers that already hold a client) Exec uses it and leaves lifecycle
	// management to the caller.
	Client BalanceClient
}

func NewGetPublicBalanceCommand(config *app.Config) *GetPublicBalanceCommand {
	return &GetPublicBalanceCommand{
		AppCommand: app.NewAppCommand(config),
	}
}

// Exec runs the public-balance lookup outside the Cobra wrapper. Exported so
// tests can invoke it directly after wiring Client / token.
func (c *GetPublicBalanceCommand) Exec(ctx context.Context) error {
	if c.Config.KeySecp == nil {
		return fmt.Errorf("Secp256k1 key not found in the wallet")
	}

	tokenInfo, err := c.Config.Tokens.ResolveToken(c.token)
	if err != nil {
		return err
	}

	if tokenInfo.Address != ETH_TOKEN {
		// TODO: implement ERC-20 balanceOf via generated bindings using c.Client.
		return fmt.Errorf("ERC-20 public balance query not yet implemented for %s", tokenInfo.Symbol)
	}

	client, cleanup, err := c.resolveClient(ctx)
	if err != nil {
		return fmt.Errorf("connecting to rpc node: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	account := common.HexToAddress(c.Config.KeySecp.PublicKey().Address())
	balance, err := client.BalanceAt(ctx, account, nil)
	if err != nil {
		return fmt.Errorf("failed to get balance: %w", err)
	}

	fmt.Println(c.Config.Tokens.FormatAmount(balance, tokenInfo))
	return nil
}

// resolveClient returns the injected client if one was set, otherwise dials
// Config.RpcUrl. The cleanup func is nil for the injected path (caller owns
// lifecycle) and closes the dialed client for the production path.
func (c *GetPublicBalanceCommand) resolveClient(ctx context.Context) (BalanceClient, func(), error) {
	if c.Client != nil {
		return c.Client, nil, nil
	}
	dialed, err := ethclient.DialContext(ctx, c.Config.RpcUrl)
	if err != nil {
		return nil, nil, err
	}
	return dialed, func() { dialed.Close() }, nil
}

func (c *GetPublicBalanceCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getpublicbalance",
		Short: `display public balance of this wallet`,
		Long:  `display public balance of this wallet (ETH or ERC-20 token)`,

		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.token, "token", "k", "", "Token symbol or address (default: ETH)")
	return cmd
}
