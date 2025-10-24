package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"math/big"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	"github.com/spf13/cobra"
)


type DepositCommand struct {
	*app.ChainCommand
	value string
}


func NewDepositCommand(config *app.Config, blockchainClient blockchain.Client) *DepositCommand {
	cmd := &DepositCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}

	return cmd
}

func (c *DepositCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deposit",
		Short: `deposit fund into the PES system`,
		Long:  `deposit fund into the PES system`,
		Run: func(cmd *cobra.Command, args []string)  {

			amount, err := ParseEtherValue(c.value)
			if err != nil {
				fmt.Printf("Error: invalid amount: %v\n", err)
				return
			}
			if c.BlockchainClient == nil {
				//create blockchain client
				if err := c.InitChainClient(); err != nil {
					fmt.Printf("Error connecting to rpc node: %v\n", err)
					return 

				}
			} 
			defer c.CloseClient()

			var payload []byte
			appId := big.NewInt(1)
			var protocolVersion uint8 = 0
			requestType := common.Process
			requestID, blockNumber, err := c.BlockchainClient.SubmitRequest(context.Background(), protocolVersion, appId, requestType, payload, amount)
			if err != nil {
				fmt.Printf("Error sending request to deposit amount %s: %v", c.value, err)
				return 
			}

			fmt.Println("Waiting for confirmation from PES")


			result, err := c.WaitForRequestCompleted(requestID, blockNumber)
			if err != nil {
				fmt.Println(err)
				return
			}
			
			if result {
				fmt.Println("Deposit completed successfully")
			} else {
				fmt.Println("Deposit failed")
			}

			
		},
	}
	cmd.Flags().StringVarP(&c.value, "amount", "a", "", "The amount of Ether to process (e.g., 1.5 ETH). It can be specified in ETH, Wei or GWei. Eg --amount 147777 Wei")
	return cmd
}


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

