package app

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/magiconair/properties"
)

// TokenInfo describes a token known to the wallet.
type TokenInfo struct {
	Symbol   string
	Address  ethCommon.Address
	Decimals uint8
}

// ethTokenInfo is the implicit ETH entry — always available, never in config.
var ethTokenInfo = TokenInfo{
	Symbol:   "ETH",
	Address:  ethCommon.Address{},
	Decimals: 18,
}

// TokenRegistry maps symbols and addresses to token metadata.
type TokenRegistry struct {
	bySymbol  map[string]*TokenInfo // upper-case symbol -> info
	byAddress map[string]*TokenInfo // lower-case hex address -> info
}

// LoadTokenRegistry reads token entries from a properties config.
// Tokens are defined as:
//
//	token.USDC.address=0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48
//	token.USDC.decimals=6
//
// ETH is always implicitly registered (address 0x0, 18 decimals).
func LoadTokenRegistry(config *properties.Properties) (*TokenRegistry, error) {
	r := &TokenRegistry{
		bySymbol:  make(map[string]*TokenInfo),
		byAddress: make(map[string]*TokenInfo),
	}

	// ETH is always present
	eth := ethTokenInfo // copy
	r.bySymbol["ETH"] = &eth
	r.byAddress[strings.ToLower(eth.Address.Hex())] = &eth

	if config == nil {
		return r, nil
	}

	// Discover token symbols by scanning for "token.<SYMBOL>.address" keys
	symbols := map[string]bool{}
	for _, key := range config.Keys() {
		if strings.HasPrefix(key, "token.") {
			parts := strings.SplitN(key, ".", 3)
			if len(parts) == 3 {
				symbols[parts[1]] = true
			}
		}
	}

	for sym := range symbols {
		addrKey := fmt.Sprintf("token.%s.address", sym)
		decimalsKey := fmt.Sprintf("token.%s.decimals", sym)

		addrStr := config.GetString(addrKey, "")
		if addrStr == "" {
			return nil, fmt.Errorf("token %s: missing address (expected %s)", sym, addrKey)
		}
		if !ethCommon.IsHexAddress(addrStr) {
			return nil, fmt.Errorf("token %s: invalid address %q", sym, addrStr)
		}
		addr := ethCommon.HexToAddress(addrStr)
		if addr == (ethCommon.Address{}) {
			return nil, fmt.Errorf("token %s: zero address is reserved for ETH", sym)
		}

		decimalsStr := config.GetString(decimalsKey, "")
		if decimalsStr == "" {
			return nil, fmt.Errorf("token %s: missing decimals (expected %s)", sym, decimalsKey)
		}
		decimals, err := strconv.ParseUint(decimalsStr, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("token %s: invalid decimals %q: %w", sym, decimalsStr, err)
		}

		upperSym := strings.ToUpper(sym)
		if _, exists := r.bySymbol[upperSym]; exists {
			return nil, fmt.Errorf("duplicate token symbol: %s", sym)
		}
		lowerAddr := strings.ToLower(addr.Hex())
		if _, exists := r.byAddress[lowerAddr]; exists {
			return nil, fmt.Errorf("duplicate token address: %s", addr.Hex())
		}

		t := &TokenInfo{
			Symbol:   strings.ToUpper(sym),
			Address:  addr,
			Decimals: uint8(decimals),
		}
		r.bySymbol[upperSym] = t
		r.byAddress[lowerAddr] = t
	}

	return r, nil
}

// ResolveToken resolves a symbol (e.g. "USDC") or hex address to TokenInfo.
// An empty string resolves to ETH. A nil registry resolves only ETH.
func (r *TokenRegistry) ResolveToken(symbolOrAddress string) (*TokenInfo, error) {
	if r == nil {
		eth := ethTokenInfo
		if symbolOrAddress == "" {
			return &eth, nil
		}
		return nil, fmt.Errorf("token registry not configured; only ETH is available")
	}

	if symbolOrAddress == "" {
		return r.bySymbol["ETH"], nil
	}

	// Try symbol first (case-insensitive)
	if t, ok := r.bySymbol[strings.ToUpper(symbolOrAddress)]; ok {
		return t, nil
	}

	// Try address
	if ethCommon.IsHexAddress(symbolOrAddress) {
		lowerAddr := strings.ToLower(ethCommon.HexToAddress(symbolOrAddress).Hex())
		if t, ok := r.byAddress[lowerAddr]; ok {
			return t, nil
		}
		return nil, fmt.Errorf("token %s is not in the wallet registry (add it to wallet.conf)", symbolOrAddress)
	}

	return nil, fmt.Errorf("unknown token: %q (not a known symbol or valid address)", symbolOrAddress)
}

// AllTokenAddresses returns the addresses of all registered ERC-20 tokens (excluding ETH).
func (r *TokenRegistry) AllTokenAddresses() []string {
	var addrs []string
	for _, t := range r.bySymbol {
		if t.Address != (ethCommon.Address{}) {
			addrs = append(addrs, strings.ToLower(t.Address.Hex()))
		}
	}
	return addrs
}

// FormatAmount formats a raw token amount with the correct number of decimals.
// Example: FormatAmount(100000000, usdcInfo) → "100 USDC"
// Safe to call on a nil registry.
func (r *TokenRegistry) FormatAmount(amount *big.Int, token *TokenInfo) string {
	return FormatTokenAmount(amount, token.Decimals, token.Symbol)
}

// ParseAmount parses a decimal string (e.g. "100.5") into raw token units
// using the token's decimals. The string must not include a unit suffix.
// Safe to call on a nil registry.
func (r *TokenRegistry) ParseAmount(amountStr string, token *TokenInfo) (*big.Int, error) {
	return ParseTokenAmount(amountStr, token.Decimals)
}

// FormatTokenAmount formats a raw amount with the given decimals and symbol.
func FormatTokenAmount(amount *big.Int, decimals uint8, symbol string) string {
	s := amount.String()
	d := int(decimals)

	if len(s) <= d {
		s = strings.Repeat("0", d-len(s)+1) + s
	}

	intPart := s[:len(s)-d]
	fracPart := strings.TrimRight(s[len(s)-d:], "0")

	formatted := intPart
	if fracPart != "" {
		formatted += "." + fracPart
	}

	if symbol != "" {
		return formatted + " " + symbol
	}
	return formatted
}

// ParseTokenAmount parses a decimal string into raw units for the given decimals.
// E.g. ParseTokenAmount("100.5", 6) → 100500000
func ParseTokenAmount(amountStr string, decimals uint8) (*big.Int, error) {
	amountStr = strings.TrimSpace(amountStr)
	if amountStr == "" {
		return nil, fmt.Errorf("empty amount")
	}

	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)

	var integerPartStr, fractionalPartStr string
	if strings.Contains(amountStr, ".") {
		parts := strings.SplitN(amountStr, ".", 2)
		integerPartStr, fractionalPartStr = parts[0], parts[1]
		if integerPartStr == "" {
			integerPartStr = "0"
		}
	} else {
		integerPartStr = amountStr
		fractionalPartStr = ""
	}

	integerPart, ok := new(big.Int).SetString(integerPartStr, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer part: %q", integerPartStr)
	}

	weiFromInteger := new(big.Int).Mul(integerPart, multiplier)

	if fractionalPartStr == "" || strings.Trim(fractionalPartStr, "0") == "" {
		return weiFromInteger, nil
	}

	if len(fractionalPartStr) > int(decimals) {
		return nil, fmt.Errorf("too many decimal places: %q has %d but token has %d decimals", amountStr, len(fractionalPartStr), decimals)
	}

	fractionalPart, ok := new(big.Int).SetString(fractionalPartStr, 10)
	if !ok {
		return nil, fmt.Errorf("invalid fractional part: %q", fractionalPartStr)
	}

	numDecimalPlaces := len(fractionalPartStr)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(numDecimalPlaces)), nil)
	weiFromFractional := new(big.Int).Mul(fractionalPart, multiplier)
	weiFromFractional.Div(weiFromFractional, divisor)

	return new(big.Int).Add(weiFromInteger, weiFromFractional), nil
}
