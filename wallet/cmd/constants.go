package cmd

import (
	"context"
	"math/big"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/spf13/cobra"
)

// ETH_TOKEN is the package-local alias for the canonical native-token sentinel
// in vela-common-go. Matches Structs.sol: address constant ETH_TOKEN = address(0).
var ETH_TOKEN = velacommon.ETH_TOKEN

const PROTOCOL_VERSION uint8 = 0
const BLOCK_BATCH_SIZE = 100000

// resolveContext returns cmd.Context() when set, falling back to
// context.Background() otherwise. Cobra leaves c.ctx nil unless the caller
// went through ExecuteContext, so every command's Run handler needs this
// defense before passing ctx to downstream code that may not tolerate nil.
func resolveContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// parseAssetAmount parses a user-supplied amount into a *big.Int, routing to
// the ETH parser for native ETH and to the token registry's ERC-20 parser
// otherwise. Keeps the ETH/ERC-20 branching in one place for commands that
// accept an asset amount (deposit, withdraw).
func parseAssetAmount(tokens *app.TokenRegistry, info *app.TokenInfo, value string) (*big.Int, error) {
	if info.Address == ETH_TOKEN {
		return app.ParseEtherValue(value)
	}
	return tokens.ParseAmount(value, info)
}
