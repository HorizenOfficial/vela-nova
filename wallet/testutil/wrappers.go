package testutil

import (
	"context"
	"fmt"
	"math/big"

	"github.com/HorizenOfficial/vela-nova/wallet/cmd"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/stretchr/testify/require"
)

// DeployApp uploads a WASM artifact via the authority service, submits a
// deploy request on-chain, and waits for completion. Persists the assigned
// ApplicationID into the temp wallet.conf so subsequent commands pick it up.
// Returns the assigned ApplicationID.
//
// allowedTokens may be nil (ETH is always allowed). ERC-20 symbols require
// those tokens to be registered in the driver's conf; that lands with Phase 5.
func (d *WalletDriver) DeployApp(ctx context.Context, wasmPath, maxFee string, allowedTokens []string) (common.ApplicationIdType, error) {
	d.t.Helper()

	cfg := d.loadConfig()
	c := cmd.NewDeployAppCommand(cfg, d.newUserClient(), d.confPath)
	cobraCmd := c.Command()
	require.NoError(d.t, cobraCmd.Flags().Set("wasm", wasmPath))
	require.NoError(d.t, cobraCmd.Flags().Set("max-value-fee", maxFee))
	for _, tok := range allowedTokens {
		require.NoError(d.t, cobraCmd.Flags().Set("allowed-tokens", tok))
	}
	c.SubgraphClient = d.suite.GetSubgraph()

	if err := c.Exec(ctx); err != nil {
		return 0, fmt.Errorf("wallet driver: DeployApp: %w", err)
	}

	// Re-read the conf; the command wrote ApplicationID back to disk.
	return d.loadConfig().ApplicationID, nil
}

// RegisterUser submits an AssociateKey request linking the wallet's secp
// address to its P521 public key, so the enclave can encrypt events for it.
func (d *WalletDriver) RegisterUser(ctx context.Context, maxFee string) error {
	d.t.Helper()

	cfg := d.loadConfig()
	c := cmd.NewRegisterUserCommand(cfg, d.newUserClient())
	cobraCmd := c.Command()
	require.NoError(d.t, cobraCmd.Flags().Set("max-value-fee", maxFee))
	c.SubgraphClient = d.suite.GetSubgraph()

	if err := c.Exec(ctx); err != nil {
		return fmt.Errorf("wallet driver: RegisterUser: %w", err)
	}
	return nil
}

// Deposit submits a deposit request for the given token and amount.
// For ETH, amount is an ether string (e.g. "1.5 ETH", "100 wei"); for ERC-20,
// it's a decimal count in the token's configured decimals.
func (d *WalletDriver) Deposit(ctx context.Context, amount, token, maxFee string) error {
	d.t.Helper()

	cfg := d.loadConfig()
	c := cmd.NewDepositCommand(cfg, d.newUserClient())
	cobraCmd := c.Command()
	require.NoError(d.t, cobraCmd.Flags().Set("amount", amount))
	require.NoError(d.t, cobraCmd.Flags().Set("max-value-fee", maxFee))
	if token != "" {
		require.NoError(d.t, cobraCmd.Flags().Set("token", token))
	}
	c.SubgraphClient = d.suite.GetSubgraph()

	if err := c.Exec(ctx); err != nil {
		return fmt.Errorf("wallet driver: Deposit: %w", err)
	}
	return nil
}

// Withdraw submits a withdraw request moving `amount` of `token` from the
// wallet's private balance to `to`. Funds land in pendingClaims on-chain and
// must be claimed via ClaimPendingPayments to reach the user's public balance.
func (d *WalletDriver) Withdraw(ctx context.Context, amount, to, token, maxFee string) error {
	d.t.Helper()

	cfg := d.loadConfig()
	c := cmd.NewWithdrawCommand(cfg, d.newUserClient())
	cobraCmd := c.Command()
	require.NoError(d.t, cobraCmd.Flags().Set("amount", amount))
	require.NoError(d.t, cobraCmd.Flags().Set("to", to))
	require.NoError(d.t, cobraCmd.Flags().Set("max-value-fee", maxFee))
	if token != "" {
		require.NoError(d.t, cobraCmd.Flags().Set("token", token))
	}
	c.SubgraphClient = d.suite.GetSubgraph()

	if err := c.Exec(ctx); err != nil {
		return fmt.Errorf("wallet driver: Withdraw: %w", err)
	}
	return nil
}

// GetPrivateBalance returns the wallet's encrypted private balance for the
// given token, decrypted via the wallet's P521 key. Returns nil if no balance
// was found within the configured scan depth (distinct from a zero balance).
func (d *WalletDriver) GetPrivateBalance(ctx context.Context, token string) (*big.Int, error) {
	d.t.Helper()

	cfg := d.loadConfig()
	c := cmd.NewGetPrivateBalanceCommand(cfg, d.newUserClient())
	cobraCmd := c.Command()
	if token != "" {
		require.NoError(d.t, cobraCmd.Flags().Set("token", token))
	}
	c.SubgraphClient = d.suite.GetSubgraph()

	balance, _, _, err := c.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("wallet driver: GetPrivateBalance: %w", err)
	}
	return balance, nil
}

// ClaimPendingPayments pulls pending claims for `token` from the
// ProcessorEndpoint contract into the user's public balance.
func (d *WalletDriver) ClaimPendingPayments(ctx context.Context, token string) error {
	d.t.Helper()

	cfg := d.loadConfig()
	c := cmd.NewClaimPendingPaymentsCommand(cfg, d.newUserClient())
	cobraCmd := c.Command()
	if token != "" {
		require.NoError(d.t, cobraCmd.Flags().Set("token", token))
	}
	c.SubgraphClient = d.suite.GetSubgraph()

	if err := c.Exec(ctx); err != nil {
		return fmt.Errorf("wallet driver: ClaimPendingPayments: %w", err)
	}
	return nil
}
