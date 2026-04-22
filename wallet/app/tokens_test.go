package app

import (
	"math/big"
	"strings"
	"testing"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/magiconair/properties"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Synthetic fake fixture addresses, these are developer-only test values
const (
	usdcAddrHex = "0xcafeBABE00000000000000000000000000000001"
	usdtAddrHex = "0xdeadBEEF00000000000000000000000000000002"
)

// loadProps builds a *properties.Properties from the given multi-line string.
func loadProps(t *testing.T, s string) *properties.Properties {
	t.Helper()
	p, err := properties.LoadString(s)
	require.NoError(t, err)
	return p
}

// ---------- LoadTokenRegistry ----------

func TestLoadTokenRegistry_NilConfig_OnlyETH(t *testing.T) {
	r, err := LoadTokenRegistry(nil)
	require.NoError(t, err)
	require.NotNil(t, r)

	eth, err := r.ResolveToken("ETH")
	require.NoError(t, err)
	require.Equal(t, "ETH", eth.Symbol)
	require.Equal(t, uint8(18), eth.Decimals)
	require.Equal(t, ethCommon.Address{}, eth.Address)

	// No ERC-20 tokens are registered.
	require.Empty(t, r.AllTokenAddresses())
}

func TestLoadTokenRegistry_EmptyConfig_OnlyETH(t *testing.T) {
	r, err := LoadTokenRegistry(loadProps(t, ""))
	require.NoError(t, err)

	eth, err := r.ResolveToken("")
	require.NoError(t, err)
	require.Equal(t, "ETH", eth.Symbol)
	require.Empty(t, r.AllTokenAddresses())
}

func TestLoadTokenRegistry_SingleToken(t *testing.T) {
	cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n"
	r, err := LoadTokenRegistry(loadProps(t, cfg))
	require.NoError(t, err)

	usdc, err := r.ResolveToken("USDC")
	require.NoError(t, err)
	require.Equal(t, "USDC", usdc.Symbol)
	require.Equal(t, uint8(6), usdc.Decimals)
	require.Equal(t, ethCommon.HexToAddress(usdcAddrHex), usdc.Address)

	addrs := r.AllTokenAddresses()
	require.Len(t, addrs, 1)
	assert.Equal(t, strings.ToLower(usdcAddrHex), addrs[0])
}

func TestLoadTokenRegistry_MultipleTokens(t *testing.T) {
	cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n" +
		"token.USDT.address=" + usdtAddrHex + "\n" +
		"token.USDT.decimals=6\n"
	r, err := LoadTokenRegistry(loadProps(t, cfg))
	require.NoError(t, err)

	_, err = r.ResolveToken("USDC")
	require.NoError(t, err)
	_, err = r.ResolveToken("USDT")
	require.NoError(t, err)

	addrs := r.AllTokenAddresses()
	require.Len(t, addrs, 2)
	assert.Contains(t, addrs, strings.ToLower(usdcAddrHex))
	assert.Contains(t, addrs, strings.ToLower(usdtAddrHex))
}

// TestLoadTokenRegistry_SymbolCaseNormalizedToUpper pins the invariant that
// symbol lookups are case-insensitive: whatever case the config declares, the
// registry stores the canonical upper-case form and resolves any casing.
func TestLoadTokenRegistry_SymbolCaseNormalizedToUpper(t *testing.T) {
	// Config uses lowercase "usdc" — registry should store as "USDC".
	cfg := "token.usdc.address=" + usdcAddrHex + "\n" +
		"token.usdc.decimals=6\n"
	r, err := LoadTokenRegistry(loadProps(t, cfg))
	require.NoError(t, err)

	// Resolvable via upper, lower, and mixed case.
	for _, q := range []string{"USDC", "usdc", "UsDc"} {
		t.Run(q, func(t *testing.T) {
			tk, err := r.ResolveToken(q)
			require.NoError(t, err)
			require.Equal(t, "USDC", tk.Symbol)
		})
	}
}

func TestLoadTokenRegistry_MissingAddress(t *testing.T) {
	cfg := "token.USDC.decimals=6\n"
	_, err := LoadTokenRegistry(loadProps(t, cfg))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing address")
}

func TestLoadTokenRegistry_MissingDecimals(t *testing.T) {
	cfg := "token.USDC.address=" + usdcAddrHex + "\n"
	_, err := LoadTokenRegistry(loadProps(t, cfg))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing decimals")
}

func TestLoadTokenRegistry_InvalidAddress(t *testing.T) {
	cfg := "token.USDC.address=not-an-address\n" +
		"token.USDC.decimals=6\n"
	_, err := LoadTokenRegistry(loadProps(t, cfg))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid address")
}

// TestLoadTokenRegistry_ZeroAddressReserved guards against a user registering
// an ERC-20 at address 0x0, which is reserved for ETH across the whole system.
// Allowing it would silently shadow the implicit ETH entry.
func TestLoadTokenRegistry_ZeroAddressReserved(t *testing.T) {
	// The zero address is always ETH and cannot be registered as an ERC-20.
	cfg := "token.FAKEETH.address=0x0000000000000000000000000000000000000000\n" +
		"token.FAKEETH.decimals=18\n"
	_, err := LoadTokenRegistry(loadProps(t, cfg))
	require.Error(t, err)
	require.Contains(t, err.Error(), "zero address is reserved for ETH")
}

func TestLoadTokenRegistry_InvalidDecimals(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"non-numeric", "abc"},
		{"negative", "-1"},
		{"overflow-uint8", "256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
				"token.USDC.decimals=" + tc.value + "\n"
			_, err := LoadTokenRegistry(loadProps(t, cfg))
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid decimals")
		})
	}
}

// TestLoadTokenRegistry_DuplicateSymbol covers the subtle case where two
// declarations that look distinct in the raw config (e.g. "USDC" vs "usdc")
// collapse to the same canonical symbol after upper-casing — they must be
// rejected rather than silently overwriting each other.
func TestLoadTokenRegistry_DuplicateSymbol(t *testing.T) {
	// Same symbol declared with two different case-forms collapses to the same
	// upper-cased key and must be rejected.
	cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n" +
		"token.usdc.address=" + usdtAddrHex + "\n" +
		"token.usdc.decimals=6\n"
	_, err := LoadTokenRegistry(loadProps(t, cfg))
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate token symbol")
}

// TestLoadTokenRegistry_DuplicateAddress covers the complementary dimension:
// two different symbols can't both claim the same on-chain address, because
// address-based lookups would then be ambiguous.
func TestLoadTokenRegistry_DuplicateAddress(t *testing.T) {
	// Two different symbols pointing at the same token address must be rejected.
	cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n" +
		"token.USDCBIS.address=" + usdcAddrHex + "\n" +
		"token.USDCBIS.decimals=6\n"
	_, err := LoadTokenRegistry(loadProps(t, cfg))
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate token address")
}

// TestLoadTokenRegistry_NonTokenKeysIgnored guards against regressions in the
// `token.<SYMBOL>.<field>` key-scanner: wallet.conf mixes token config with
// unrelated keys (rpcUrl, ApplicationID, ...), and only keys under the
// `token.` prefix should feed token discovery.
func TestLoadTokenRegistry_NonTokenKeysIgnored(t *testing.T) {
	// Unrelated keys (e.g., rpcUrl) in the same properties file must not
	// confuse token discovery.
	cfg := "rpcUrl=https://example.com\n" +
		"ApplicationID=42\n" +
		"token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n"
	r, err := LoadTokenRegistry(loadProps(t, cfg))
	require.NoError(t, err)
	_, err = r.ResolveToken("USDC")
	require.NoError(t, err)
}

// ---------- ResolveToken ----------

func TestResolveToken_EmptyStringReturnsETH(t *testing.T) {
	r, err := LoadTokenRegistry(nil)
	require.NoError(t, err)
	tk, err := r.ResolveToken("")
	require.NoError(t, err)
	require.Equal(t, "ETH", tk.Symbol)
}

// TestResolveToken_ByAddressAnyCase pins the case-insensitive address lookup
// contract. The wallet accepts `--token <addr>` from the CLI, where users may
// paste checksummed, all-lower, or all-upper hex — all three must resolve to
// the same registered token.
func TestResolveToken_ByAddressAnyCase(t *testing.T) {
	cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n"
	r, err := LoadTokenRegistry(loadProps(t, cfg))
	require.NoError(t, err)

	cases := []string{
		usdcAddrHex,                  // checksummed
		strings.ToLower(usdcAddrHex), // lower
		strings.ToUpper(usdcAddrHex[:2]) + // 0x + rest uppercase
			strings.ToUpper(usdcAddrHex[2:]),
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			tk, err := r.ResolveToken(q)
			require.NoError(t, err)
			require.Equal(t, "USDC", tk.Symbol)
		})
	}
}

func TestResolveToken_UnknownSymbol(t *testing.T) {
	r, err := LoadTokenRegistry(nil)
	require.NoError(t, err)
	_, err = r.ResolveToken("FOO")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown token")
}

func TestResolveToken_ValidAddressNotRegistered(t *testing.T) {
	r, err := LoadTokenRegistry(nil)
	require.NoError(t, err)
	_, err = r.ResolveToken(usdcAddrHex)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in the wallet registry")
}

// TestResolveToken_NilRegistry documents the defensive nil-receiver contract
// of ResolveToken: a command invoked before a registry has been loaded can
// still resolve ETH (which is always implicit) but must fail cleanly for any
// non-empty token query rather than panic.
func TestResolveToken_NilRegistry(t *testing.T) {
	var r *TokenRegistry

	// Nil registry + empty string still returns ETH.
	tk, err := r.ResolveToken("")
	require.NoError(t, err)
	require.Equal(t, "ETH", tk.Symbol)

	// Nil registry + anything else errors.
	_, err = r.ResolveToken("USDC")
	require.Error(t, err)
	require.Contains(t, err.Error(), "token registry not configured")
}

// ---------- AllTokenAddresses ----------

// TestAllTokenAddresses_ExcludesETHAndReturnsLowercase pins two contracts
// callers rely on: (1) ETH is excluded because it lives outside the ERC-20
// allowlist model, and (2) addresses come back lower-cased so direct map
// lookups against other lower-cased hex keys work without re-normalization.
func TestAllTokenAddresses_ExcludesETHAndReturnsLowercase(t *testing.T) {
	cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n"
	r, err := LoadTokenRegistry(loadProps(t, cfg))
	require.NoError(t, err)

	addrs := r.AllTokenAddresses()
	require.Len(t, addrs, 1)
	// Lowercased, not checksummed.
	assert.Equal(t, strings.ToLower(usdcAddrHex), addrs[0])
	assert.NotContains(t, addrs, ethCommon.Address{}.Hex())
}

// ---------- FormatTokenAmount ----------

func TestFormatTokenAmount(t *testing.T) {
	cases := []struct {
		name     string
		amount   *big.Int
		decimals uint8
		symbol   string
		want     string
	}{
		{"zero_ETH", big.NewInt(0), 18, "ETH", "0 ETH"},
		{"1_wei", big.NewInt(1), 18, "ETH", "0.000000000000000001 ETH"},
		{"1_ETH", new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), 18, "ETH", "1 ETH"},
		{"1.5_ETH", big.NewInt(15e17), 18, "ETH", "1.5 ETH"},
		{"zero_USDC", big.NewInt(0), 6, "USDC", "0 USDC"},
		{"1_USDC", big.NewInt(1_000_000), 6, "USDC", "1 USDC"},
		{"1.5_USDC", big.NewInt(1_500_000), 6, "USDC", "1.5 USDC"},
		{"100_USDC", big.NewInt(100_000_000), 6, "USDC", "100 USDC"},
		{"sub_unit_USDC", big.NewInt(1), 6, "USDC", "0.000001 USDC"},
		{"trailing_zeros_trimmed", big.NewInt(1_200_000), 6, "USDC", "1.2 USDC"},
		{"no_symbol", big.NewInt(1_500_000), 6, "", "1.5"},
		{"zero_decimals_raw", big.NewInt(100), 0, "UNIT", "100 UNIT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatTokenAmount(tc.amount, tc.decimals, tc.symbol)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------- ParseTokenAmount ----------

func TestParseTokenAmount_Valid(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		decimals uint8
		want     *big.Int
	}{
		{"integer_ETH", "1", 18, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)},
		{"integer_USDC", "100", 6, big.NewInt(100_000_000)},
		{"fractional_half_USDC", "0.5", 6, big.NewInt(500_000)},
		{"fractional_leading_dot", ".5", 6, big.NewInt(500_000)},
		{"smallest_USDC_unit", "0.000001", 6, big.NewInt(1)},
		{"zero", "0", 6, big.NewInt(0)},
		{"trailing_zeros_fraction_equivalent_to_integer", "1.000", 6, big.NewInt(1_000_000)},
		{"whitespace_trimmed", "  1.5  ", 6, big.NewInt(1_500_000)},
		{"large_value_USDC", "123456789", 6, big.NewInt(123_456_789_000_000)},
		{"zero_decimals_raw", "42", 0, big.NewInt(42)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTokenAmount(tc.input, tc.decimals)
			require.NoError(t, err)
			require.Equal(t, 0, tc.want.Cmp(got), "expected %s, got %s", tc.want, got)
		})
	}
}

func TestParseTokenAmount_InvalidInput(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		decimals uint8
		wantMsg  string
	}{
		{"empty", "", 6, "empty amount"},
		{"whitespace_only", "   ", 6, "empty amount"},
		{"too_many_decimals", "1.1234567", 6, "too many decimal places"},
		{"non_numeric_integer", "abc", 6, "invalid integer part"},
		{"non_numeric_fractional", "1.abc", 6, "invalid fractional part"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTokenAmount(tc.input, tc.decimals)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// TestParseFormatTokenAmount_RoundTrip ensures that amounts the user types in
// (Parse) and amounts the wallet displays (Format) are exact inverses at both
// 6- and 18-decimal precisions. A drift between the two would produce
// "displayed balance doesn't match what I deposited" bugs.
func TestParseFormatTokenAmount_RoundTrip(t *testing.T) {
	// Round-trip a handful of values through Parse → Format → Parse for both
	// USDC (6 decimals) and ETH (18 decimals).
	cases := []struct {
		name     string
		input    string
		decimals uint8
		symbol   string
	}{
		{"USDC_integer", "100", 6, "USDC"},
		{"USDC_fractional", "1.5", 6, "USDC"},
		{"USDC_smallest_unit", "0.000001", 6, "USDC"},
		{"ETH_fractional", "1.5", 18, "ETH"},
		{"ETH_wei_precision", "0.000000000000000001", 18, "ETH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := ParseTokenAmount(tc.input, tc.decimals)
			require.NoError(t, err)

			formatted := FormatTokenAmount(raw, tc.decimals, "")
			assert.Equal(t, tc.input, formatted, "round-trip Parse→Format should preserve input")

			raw2, err := ParseTokenAmount(formatted, tc.decimals)
			require.NoError(t, err)
			require.Equal(t, 0, raw.Cmp(raw2), "Parse→Format→Parse should be stable")
		})
	}
}

// ---------- Registry ParseAmount / FormatAmount wrappers ----------

func TestRegistryAmountHelpers(t *testing.T) {
	cfg := "token.USDC.address=" + usdcAddrHex + "\n" +
		"token.USDC.decimals=6\n"
	r, err := LoadTokenRegistry(loadProps(t, cfg))
	require.NoError(t, err)

	usdc, err := r.ResolveToken("USDC")
	require.NoError(t, err)

	raw, err := r.ParseAmount("1.5", usdc)
	require.NoError(t, err)
	assert.Equal(t, 0, big.NewInt(1_500_000).Cmp(raw))

	formatted := r.FormatAmount(raw, usdc)
	assert.Equal(t, "1.5 USDC", formatted)
}

// TestRegistryAmountHelpers_NilRegistrySafeWithToken pins the documented
// nil-receiver safety of (*TokenRegistry).ParseAmount / FormatAmount: callers
// that already hold a *TokenInfo (e.g. the always-implicit ETH) must be able
// to format/parse amounts without first loading a registry.
func TestRegistryAmountHelpers_NilRegistrySafeWithToken(t *testing.T) {
	// The package comment on FormatAmount/ParseAmount states these are safe to
	// call on a nil registry as long as a valid *TokenInfo is passed.
	var r *TokenRegistry
	eth := &TokenInfo{Symbol: "ETH", Decimals: 18}

	raw, err := r.ParseAmount("1.5", eth)
	require.NoError(t, err)
	assert.Equal(t, 0, big.NewInt(15e17).Cmp(raw))

	formatted := r.FormatAmount(raw, eth)
	assert.Equal(t, "1.5 ETH", formatted)
}
