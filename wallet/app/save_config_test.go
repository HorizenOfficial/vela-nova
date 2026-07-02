package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/magiconair/properties"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveConfigToFile_RoundTrip verifies that a Config survives the
// SaveConfigToFile → LoadConfigFromFile round-trip for the simple fields
// (keys, URLs, ApplicationID, polling settings).
func TestSaveConfigToFile_RoundTrip(t *testing.T) {
	keySecp, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	keyP521, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	original := &Config{
		KeySecp:                   keySecp,
		KeyP521:                   keyP521,
		RpcUrl:                    "http://localhost:8545",
		AuthorityServiceURL:       "http://localhost:9000",
		SubgraphURL:               "http://localhost:8000/subgraphs",
		ApplicationID:             common.NewApplicationId(99),
		BlockchainPollingInterval: 3,
		BlockchainPollingTimeout:  30,
	}

	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, SaveConfigToFile(original, conf))

	loaded, err := LoadConfigFromFile(conf)
	require.NoError(t, err)

	assert.Equal(t, original.RpcUrl, loaded.RpcUrl)
	assert.Equal(t, original.AuthorityServiceURL, loaded.AuthorityServiceURL)
	assert.Equal(t, original.SubgraphURL, loaded.SubgraphURL)
	assert.Equal(t, original.ApplicationID, loaded.ApplicationID)
	assert.Equal(t, original.BlockchainPollingInterval, loaded.BlockchainPollingInterval)
	assert.Equal(t, original.BlockchainPollingTimeout, loaded.BlockchainPollingTimeout)
	assert.NotNil(t, loaded.KeySecp)
	assert.NotNil(t, loaded.KeyP521)
}

// TestSaveConfigToFile_TokensRoundTrip verifies that a TokenRegistry survives
// the SaveConfigToFile → LoadConfigFromFile round-trip: each token must come
// back with the same address, decimals, and symbol (upper-cased).
func TestSaveConfigToFile_TokensRoundTrip(t *testing.T) {
	// Build the original token registry via the same path user configs take.
	tokenCfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n" +
		"token.USDT.address=" + usdtAddrHex + "\n" +
		"token.USDT.decimals=6\n"
	p, err := properties.LoadString(tokenCfg)
	require.NoError(t, err)
	tokens, err := LoadTokenRegistry(p)
	require.NoError(t, err)

	original := &Config{
		RpcUrl: "http://localhost:8545",
		Tokens: tokens,
	}

	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, SaveConfigToFile(original, conf))

	loaded, err := LoadConfigFromFile(conf)
	require.NoError(t, err)
	require.NotNil(t, loaded.Tokens)

	for _, sym := range []string{"USDC", "USDT"} {
		t.Run(sym, func(t *testing.T) {
			origTok, err := original.Tokens.ResolveToken(sym)
			require.NoError(t, err)
			loadedTok, err := loaded.Tokens.ResolveToken(sym)
			require.NoError(t, err)
			assert.Equal(t, origTok.Symbol, loadedTok.Symbol)
			assert.Equal(t, origTok.Decimals, loadedTok.Decimals)
			assert.Equal(t, origTok.Address, loadedTok.Address)
		})
	}

	// AllTokenAddresses should return both (excluding ETH).
	addrs := loaded.Tokens.AllTokenAddresses()
	assert.Len(t, addrs, 2)
	assert.Contains(t, addrs, strings.ToLower(usdcAddrHex))
	assert.Contains(t, addrs, strings.ToLower(usdtAddrHex))
}

// TestSaveConfigToFile_SkipsImplicitETH verifies that ETH is never serialized —
// it is always the implicit token and must not appear as a `token.ETH.*` entry
// in the saved file.
func TestSaveConfigToFile_SkipsImplicitETH(t *testing.T) {
	tokens, err := LoadTokenRegistry(nil) // registry containing only ETH
	require.NoError(t, err)

	cfg := &Config{
		RpcUrl: "http://localhost:8545",
		Tokens: tokens,
	}

	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, SaveConfigToFile(cfg, conf))

	data, err := os.ReadFile(conf)
	require.NoError(t, err)
	content := string(data)

	assert.NotContains(t, content, "token.ETH.")
	assert.NotContains(t, content, velacommon.ETH_TOKEN.Hex())
}

// TestLoadConfigFromFile_RejectsMalformedTokens verifies that corrupt token
// stanzas cause LoadConfigFromFile to fail, so misconfiguration is surfaced
// immediately rather than silently dropping tokens.
func TestLoadConfigFromFile_RejectsMalformedTokens(t *testing.T) {
	// Minimal valid skeleton — MustGetString on these keys panics if missing.
	base := "keyP521=\n" +
		"keySecp256k1=\n" +
		"rpcUrl=http://localhost:8545\n" +
		"ProcessorAddress=\n" +
		"TeeAuthenticatorAddress=\n"

	cases := []struct {
		name    string
		extra   string
		wantMsg string
	}{
		{
			name:    "missing_address",
			extra:   "token.USDC.decimals=6\n",
			wantMsg: "missing address",
		},
		{
			name:    "missing_decimals",
			extra:   "token.USDC.address=" + usdcAddrHex + "\n",
			wantMsg: "missing decimals",
		},
		{
			name: "invalid_address",
			extra: "token.USDC.address=not-an-address\n" +
				"token.USDC.decimals=6\n",
			wantMsg: "invalid address",
		},
		{
			name: "invalid_decimals",
			extra: "token.USDC.address=" + usdcAddrHex + "\n" +
				"token.USDC.decimals=abc\n",
			wantMsg: "invalid decimals",
		},
		{
			name: "zero_address_reserved",
			extra: "token.FAKEETH.address=0x0000000000000000000000000000000000000000\n" +
				"token.FAKEETH.decimals=18\n",
			wantMsg: "zero address is reserved for ETH",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conf := filepath.Join(t.TempDir(), "wallet.conf")
			require.NoError(t, os.WriteFile(conf, []byte(base+tc.extra), 0o644))

			_, err := LoadConfigFromFile(conf)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "error loading token registry")
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}
