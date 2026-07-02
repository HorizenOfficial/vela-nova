package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/HorizenOfficial/vela-common-go/wasm/types"
	runtimeapp "github.com/HorizenOfficial/vela-nova/payment-app/app"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/spf13/cobra"
)

func NewRequestReportCommand(config *app.Config, blockchainClient blockchain.Client) *RequestReportCommand {
	return &RequestReportCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

type RequestReportCommand struct {
	*app.ChainCommand
	maxFeeValue   string
	reportType    string
	address       string
	fromTimestamp int64
	toTimestamp   int64
}

// Exec runs the requestreport flow outside of the Cobra wrapper. Exported so
// test drivers can invoke it directly after setting flag-bound struct fields.
// Returns the assigned request ID so callers can pass it to /getreport.
func (c *RequestReportCommand) Exec(ctx context.Context) (common.RequestIdType, error) {
	if err := c.RequireApplicationID(); err != nil {
		return common.RequestIdType{}, err
	}

	maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
	if err != nil {
		return common.RequestIdType{}, fmt.Errorf("invalid max fee amount: %w", err)
	}

	if err := c.InitChainClient(ctx); err != nil {
		return common.RequestIdType{}, fmt.Errorf("connecting to rpc node: %w", err)
	}
	defer c.CloseClient()

	if c.reportType != "" && c.reportType != "balances" && c.reportType != "tx_history" {
		return common.RequestIdType{}, fmt.Errorf("unsupported report type %q (must be 'balances' or 'tx_history')", c.reportType)
	}
	if c.reportType == "tx_history" && c.address == "" {
		return common.RequestIdType{}, fmt.Errorf("--address is required for tx_history report type")
	}

	deanonInstruction := runtimeapp.DeanonymizeInstruction{
		ReportType:    c.reportType,
		FromTimestamp: c.fromTimestamp,
		ToTimestamp:   c.toTimestamp,
	}
	if c.address != "" {
		addr, err := types.HexToAddress(c.address)
		if err != nil {
			return common.RequestIdType{}, fmt.Errorf("invalid address: %w", err)
		}
		deanonInstruction.Address = addr
	}

	payload := runtimeapp.PayloadInstructions{Deanonymize: &deanonInstruction}
	encryptedPayload, err := c.EncryptPayload(&payload, ctx)
	if err != nil {
		return common.RequestIdType{}, fmt.Errorf("encrypting deanonymization payload: %w", err)
	}

	requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, c.Config.ApplicationID, common.Deanonymize, encryptedPayload, ETH_TOKEN, big.NewInt(0), maxFeeValue)
	if err != nil {
		return common.RequestIdType{}, fmt.Errorf("sending request to generate a deanonymization report: %w", err)
	}

	fmt.Println("Waiting for confirmation from Vela")
	if err := c.WaitForRequestCompleted(requestID, ctx); err != nil {
		return requestID, fmt.Errorf("Deanonymization request failed: %w", err)
	}
	fmt.Printf("Deanonymization request completed successfully. Report id: %d_%s\n", c.Config.ApplicationID, requestID)
	return requestID, nil
}

func (c *RequestReportCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requestreport",
		Short: `requests a deanonymization report of the Nova app`,
		Long:  `requests a deanonymization report of the Nova app. Use --report-type to select 'balances' (default) or 'tx_history'.`,
		Run: func(cmd *cobra.Command, args []string) {
			if _, err := c.Exec(resolveContext(cmd)); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	cmd.Flags().StringVar(&c.reportType, "report-type", "", "Report type: 'balances' (default) or 'tx_history'")
	cmd.Flags().StringVar(&c.address, "address", "", "Address to query (required for tx_history)")
	cmd.Flags().Int64Var(&c.fromTimestamp, "from-timestamp", 0, "Filter tx_history from this Unix timestamp (inclusive)")
	cmd.Flags().Int64Var(&c.toTimestamp, "to-timestamp", 0, "Filter tx_history up to this Unix timestamp (inclusive)")
	return cmd
}
