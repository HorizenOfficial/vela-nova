package app

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEtherValue(t *testing.T) {


	input := ""
	_, err := ParseEtherValue(input)
	require.Error(t, err)

	input = "34445 wei"
	value, err := ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(34445), value)

	input = "34445 Wei"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(34445), value)

	input = "34445 WEI"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(34445), value)

	input = "WEI 34445 "
	value, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "34445"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "1.5 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(15e17), value)

	input = "100 Gwei"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100e9), value)

	input = "100.0 GWEI"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100e9), value)

	input = "100.0 gwei"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(100e9), value)

	input = "0.000000001 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1e9), value)

	input = "  5.0     GWEI  "
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(5e9), value)

	input = "invalid input"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "100 USD"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "0.002 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(2e15), value)

	input = "0.00200 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(2e15), value)

	input = ".5 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(5e17), value)

	input = "1.5.4 ETH"
	value, err = ParseEtherValue(input)
	require.Error(t, err)

	// Smallest valid ETH value (1 wei)
	input = "0.000000000000000001 ETH"
	value, err = ParseEtherValue(input)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1), value)

	// Below 1 wei
	input = "0.0000000000000000001 ETH"
	value, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "0.2000000001 Gwei"
	value, err = ParseEtherValue(input)
	require.Error(t, err)
	
	input = "1.5 Wei"
	_, err = ParseEtherValue(input)
	require.Error(t, err)

	input = "-1 Wei"
	_, err = ParseEtherValue(input)
	require.Error(t, err)
}

func TestWeiToEtherStr(t *testing.T) {
	tests := []struct {
		name     string
		wei      *big.Int
		expected string
	}{
		{"zero", big.NewInt(0), "0"},
		{"1 wei", big.NewInt(1), "0.000000000000000001"},
		{"1 gwei", big.NewInt(1e9), "0.000000001"},
		{"1 ether", new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), "1"},
		{"1.5 ether", big.NewInt(15e17), "1.5"},
		{"0.002 ether", big.NewInt(2e15), "0.002"},
		{"large value", func() *big.Int {
			v, _ := new(big.Int).SetString("123456789000000000000000000", 10)
			return v
		}(), "123456789"},
		{"all decimals", func() *big.Int {
			v, _ := new(big.Int).SetString("1234567890123456789", 10)
			return v
		}(), "1.234567890123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WeiToEtherStr(tt.wei)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestWeiToEtherStrRoundTrip(t *testing.T) {
	// Verify WeiToEtherStr + ParseEtherValue round-trips correctly
	original := big.NewInt(15e17) // 1.5 ETH
	ethStr := WeiToEtherStr(original)
	parsed, err := ParseEtherValue(ethStr + " ETH")
	require.NoError(t, err)
	require.Equal(t, 0, original.Cmp(parsed), "round-trip failed: %s", ethStr)
}

func TestValidateAndChecksumAddress(t *testing.T) {

	input := ""
	_, err := ValidateAndChecksumAddress(input)
	require.Error(t, err)

	//Too short
	input = fmt.Sprintf("0xadd%017x", 1)
	_, err = ValidateAndChecksumAddress(input)
	require.Error(t, err)

	//Too long
	input = fmt.Sprintf("0xadd%0100x", 1)
	_, err = ValidateAndChecksumAddress(input)
	require.Error(t, err)

	//Invalid chars
	input = fmt.Sprintf("0xtdd%37x", 1)
	_, err = ValidateAndChecksumAddress(input)
	require.Error(t, err)


	//Valid mixed-case (already EIP-155 checksummed)
	input = "0x12396eE1F85b76b1e33BE56d6bd0c6545d47BDEA"
	address, err := ValidateAndChecksumAddress(input)
	require.NoError(t, err)
	require.Equal(t, input, address.String())

	address, err = ValidateAndChecksumAddress(strings.ToUpper(input))
	require.NoError(t, err)
	require.Equal(t, input, address.String())

	address, err = ValidateAndChecksumAddress(strings.ToLower(input))
	require.NoError(t, err)
	require.Equal(t, input, address.String())

	//With spaces
	address, err = ValidateAndChecksumAddress("  0x12396eE1F85b76b1e33BE56d6bd0c6545d47BDEA  ")
	require.NoError(t, err)
	require.Equal(t, input, address.String())

	//Without 0x
	address, err = ValidateAndChecksumAddress("12396eE1F85b76b1e33BE56d6bd0c6545d47BDEA")
	require.NoError(t, err)
	require.Equal(t, input, address.String())

}