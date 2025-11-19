package app

import (
	"fmt"
	"regexp"
	"strings"

	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ParseEtherValue converts a string representing a value in Ether, Gwei, or Wei
// into its equivalent value in Wei as a *big.Int.
// Examples: "1 ETH" -> 10^18 Wei, "10 Gwei" -> 10^10 Wei, "500 Wei" -> 500 Wei.
// It handles optional whitespace and case-insensitive units.
func ParseEtherValue(valueStr string) (*big.Int, error) {
	// Regular expression to capture the numeric part and the unit.
	// It allows for optional whitespace around the number and unit.
	// It captures decimal numbers (e.g., "1.5 ETH") or integers (e.g., "100 wei").
	re := regexp.MustCompile(`^\s*(\d*\.?\d+)\s*(?:(ETH|ETHER|GWEI|WEI))\s*$`)

	matches := re.FindStringSubmatch(strings.ToUpper(valueStr))

	if len(matches) != 3 {
		return nil, fmt.Errorf("invalid format for Ether value: %s. Expected format: 'VALUE UNIT' (e.g., '1.5 ETH', '100 GWEI', '50 wei')", valueStr)
	}

	// matches[1] is the numeric value string (e.g., "1.5", "100")
	// matches[2] is the unit string (e.g., "ETH", "GWEI", "WEI")
	amountStr := matches[1]
	unit := matches[2]

	// Define the conversion factor (power of 10) for each unit to Wei.
	// The units are based on the standard Ethereum denominations:
	// 1 Ether = 10^18 Wei
	// 1 Gwei = 10^9 Wei (Giga-Wei)
	// 1 Wei = 10^0 Wei
	var multiplier *big.Int

	switch unit {
	case "ETH", "ETHER":
		// 10^18
		multiplier = big.NewInt(0).Exp(big.NewInt(10), big.NewInt(18), nil)
	case "GWEI":
		// 10^9
		multiplier = big.NewInt(0).Exp(big.NewInt(10), big.NewInt(9), nil)
	case "WEI":
		// 10^0 = 1
		multiplier = big.NewInt(1)
	default:
		// This should be caught by the regex, but included for safety
		return nil, fmt.Errorf("unsupported unit: %s", unit)
	}

	// Use big.Float to handle potential floating point input (e.g., "1.5 ETH")
	// The Ethereum community often uses decimals for ETH and Gwei.
	amountFloat, _, err := big.ParseFloat(amountStr, 10, 0, big.ToNearestEven)
	if err != nil {
		return nil, fmt.Errorf("failed to parse numeric part '%s': %w", amountStr, err)
	}

	// Convert the multiplier to big.Float for the multiplication
	multiplierFloat := new(big.Float).SetInt(multiplier)

	// Calculate total Wei as a big.Float: amountFloat * multiplierFloat
	weiFloat := new(big.Float).Mul(amountFloat, multiplierFloat)

	//Check if the is a fractional aprt even in wei
	weiInt, _ := weiFloat.Int(nil)
	fInt := new(big.Float).SetInt(weiInt)

    // If the original float is not equal to its integer part, it has a fraction
    if weiFloat.Cmp(fInt) != 0 {
		return nil, fmt.Errorf("cannot accept values smaller than wei '%v'", weiFloat)
	}
	return weiInt, nil
}

// ValidateAndChecksumAddress checks if the input string is a valid 
// Ethereum address and returns it in EIP-55 checksum format.
// It returns an empty string and an error if the address is invalid.
func ValidateAndChecksumAddress(address string) (common.Address, error) {
	// 1. Clean the input string
	// Remove leading/trailing whitespace and ensure we're dealing with a hex string.
	addr := strings.TrimSpace(address)
	
	// 2. Validate the length and structure
	// A valid address is 42 characters long (including '0x').
	if !common.IsHexAddress(addr) {
		return common.Address{}, fmt.Errorf("invalid address format: not a valid hex address or incorrect length")
	}


	checksummed := common.HexToAddress(addr)
	
	return checksummed, nil
}