package cmd

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/spf13/cobra"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// BalanceClient is the subset of a chain client needed to read public balances.
// BalanceAt serves the ETH path; bind.ContractBackend (specifically its
// ContractCaller.CallContract) serves the ERC-20 balanceOf path. Both
// *ethclient.Client (production) and simulated.Client (tests) satisfy it.
type BalanceClient interface {
	bind.ContractBackend
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

// erc20BalanceOfABI is the minimal ABI fragment needed to call balanceOf on
// any ERC-20. Parsed once at init — the signature is universal across tokens,
// so we do not pull in a specific token binding (which would pin us to a
// single implementation).
const erc20BalanceOfABIJSON = `[{"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

var erc20BalanceOfABI = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(erc20BalanceOfABIJSON))
	if err != nil {
		panic(fmt.Sprintf("getpublicbalance: failed to parse erc20 balanceOf ABI: %v", err))
	}
	return parsed
}()

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

	client, cleanup, err := c.resolveClient(ctx)
	if err != nil {
		return fmt.Errorf("connecting to rpc node: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	account := common.HexToAddress(c.Config.KeySecp.PublicKey().Address())

	var balance *big.Int
	if tokenInfo.Address == ETH_TOKEN {
		balance, err = client.BalanceAt(ctx, account, nil)
		if err != nil {
			return fmt.Errorf("failed to get ETH balance: %w", err)
		}
	} else {
		balance, err = erc20BalanceOf(ctx, client, tokenInfo.Address, account)
		if err != nil {
			return fmt.Errorf("failed to get %s balance: %w", tokenInfo.Symbol, err)
		}
	}

	fmt.Println(c.Config.Tokens.FormatAmount(balance, tokenInfo))
	return nil
}

// erc20BalanceOf performs a read-only balanceOf(account) call on `token` via
// an eth_call. Decodes the single uint256 return value. Relies on the ERC-20
// balanceOf signature being universal across implementations, so no
// token-specific binding is required.
func erc20BalanceOf(ctx context.Context, client bind.ContractCaller, token, account common.Address) (*big.Int, error) {
	calldata, err := erc20BalanceOfABI.Pack("balanceOf", account)
	if err != nil {
		return nil, fmt.Errorf("pack balanceOf: %w", err)
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &token, Data: calldata}, nil)
	if err != nil {
		return nil, fmt.Errorf("call balanceOf: %w", err)
	}
	values, err := erc20BalanceOfABI.Unpack("balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("unpack balanceOf: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("balanceOf returned %d values, expected 1", len(values))
	}
	balance, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("balanceOf returned unexpected type %T, expected *big.Int", values[0])
	}
	return balance, nil
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
