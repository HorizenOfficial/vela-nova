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
// This implementation avoids using floating-point arithmetic to prevent rounding errors.
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
		multiplier = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	case "GWEI":
		// 10^9
		multiplier = new(big.Int).Exp(big.NewInt(10), big.NewInt(9), nil)
	case "WEI":
		// 10^0 = 1
		multiplier = big.NewInt(1)
	default:
		// This should be caught by the regex, but included for safety
		return nil, fmt.Errorf("unsupported unit: %s", unit)
	}

	// Split the amount string into integer and fractional parts to avoid float arithmetic
	var integerPartStr, fractionalPartStr string
	if strings.Contains(amountStr, ".") {
		parts := strings.SplitN(amountStr, ".", 2)
		integerPartStr, fractionalPartStr = parts[0], parts[1]
		if integerPartStr == "" {
			integerPartStr = "0" // Handle cases like ".5 ETH"
		}
	} else {
		integerPartStr = amountStr
		fractionalPartStr = "0"
	}

	// Parse the integer part of the amount
	integerPart, ok := new(big.Int).SetString(integerPartStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse integer part '%s' from '%s'", integerPartStr, valueStr)
	}

	// Calculate the total wei from the integer part
	weiFromIntegerPart := new(big.Int).Mul(integerPart, multiplier)

	// If there's no fractional part (or it's "0"), we're done.
	if fractionalPartStr == "0" || strings.Trim(fractionalPartStr, "0") == "" {
		return weiFromIntegerPart, nil
	}

	// Parse the fractional part of the amount
	fractionalPart, ok := new(big.Int).SetString(fractionalPartStr, 10)
	if !ok {
		// This case is unlikely given the regex, but good for safety
		return nil, fmt.Errorf("failed to parse fractional part '%s' from '%s'", fractionalPartStr, valueStr)
	}

	// Calculate the value from the fractional part using integer arithmetic.
	// This is done by (fractionalPart * multiplier) / (10^numDecimalPlaces).
	// We use DivMod to check for any remainder, which would mean the value is
	// smaller than 1 Wei and thus not representable as an integer amount of Wei.
	numDecimalPlaces := len(fractionalPartStr)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(numDecimalPlaces)), nil)

	numerator := new(big.Int).Mul(fractionalPart, multiplier)
	weiFromFractionalPart, remainder := new(big.Int).DivMod(numerator, divisor, new(big.Int))

	if remainder.Cmp(big.NewInt(0)) != 0 {
		return nil, fmt.Errorf("value '%s' has a fractional wei part, which is not allowed", valueStr)
	}

	// Add the integer and fractional parts together for the total Wei value
	totalWei := new(big.Int).Add(weiFromIntegerPart, weiFromFractionalPart)

	return totalWei, nil
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