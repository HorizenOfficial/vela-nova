package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/spf13/cobra"
)

type DeployAppCommand struct {
	*app.ChainCommand
	maxFeeValue string
	wasmPath    string
}

func NewDeployAppCommand(config *app.Config, blockchainClient blockchain.Client) *DeployAppCommand {
	return &DeployAppCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
	}
}

func (c *DeployAppCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployapp",
		Short: `Uploads a wasm artifact and triggers app deployment`,
		Long:  `Uploads a wasm artifact to the authority service and submits a deploy request with an artifact descriptor payload.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.run(context.Background()); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	cmd.Flags().StringVar(&c.wasmPath, "wasm", "", "Path to wasm module file to upload and deploy")
	_ = cmd.MarkFlagRequired("wasm")
	return cmd
}

type deployDescriptorPayload struct {
	Mode       string `json:"mode"`
	ArtifactID string `json:"artifactId"`
	WasmSHA256 string `json:"wasmSha256"`
}

func (c *DeployAppCommand) run(ctx context.Context) error {
	maxFeeValue, err := app.ParseEtherValue(c.maxFeeValue)
	if err != nil {
		return fmt.Errorf("invalid max fee amount: %w", err)
	}

	if strings.TrimSpace(c.wasmPath) == "" {
		return fmt.Errorf("--wasm is required")
	}

	serviceURL, err := c.resolveArtifactServiceURL()
	if err != nil {
		return err
	}

	wasmBytes, err := os.ReadFile(c.wasmPath)
	if err != nil {
		return fmt.Errorf("failed to read wasm file %q: %w", c.wasmPath, err)
	}
	if len(wasmBytes) == 0 {
		return fmt.Errorf("wasm file %q is empty", c.wasmPath)
	}

	localSHABytes := sha256.Sum256(wasmBytes)
	localSHA := hex.EncodeToString(localSHABytes[:])
	expectedArtifactID := "sha256:" + localSHA

	authorityClient := app.NewAuthorityClient(serviceURL, 0, NOVA_APPLICATION_ID, nil)
	uploadResp, err := authorityClient.UploadWASM(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to upload wasm artifact: %w", err)
	}
	if uploadResp.WasmSHA256 != localSHA {
		return fmt.Errorf("deploy upload hash mismatch: local=%s remote=%s", localSHA, uploadResp.WasmSHA256)
	}
	if uploadResp.ArtifactID != expectedArtifactID {
		return fmt.Errorf("deploy upload artifactId mismatch: expected=%s remote=%s", expectedArtifactID, uploadResp.ArtifactID)
	}

	deployPayload, err := json.Marshal(deployDescriptorPayload{
		Mode:       "artifact_ref",
		ArtifactID: uploadResp.ArtifactID,
		WasmSHA256: uploadResp.WasmSHA256,
	})
	if err != nil {
		return fmt.Errorf("failed to encode deploy descriptor payload: %w", err)
	}

	if err := c.InitChainClient(ctx); err != nil {
		return fmt.Errorf("error connecting to rpc node: %w", err)
	}
	defer c.BlockchainClient.Close()

	requestType := common.Deploy
	requestID, _, err := c.BlockchainClient.SubmitRequest(ctx, PROTOCOL_VERSION, NOVA_APPLICATION_ID, requestType, deployPayload, ethCommon.Address{}, big.NewInt(0), maxFeeValue)
	if err != nil {
		return fmt.Errorf("error sending request to deploy app: %w", err)
	}

	fmt.Println("Waiting for confirmation from PES")
	if err := c.WaitForRequestCompleted(requestID, ctx); err != nil {
		return fmt.Errorf("deploy app failed: %w", err)
	}

	fmt.Println("Deploy app completed successfully")
	return nil
}

func (c *DeployAppCommand) resolveArtifactServiceURL() (string, error) {
	if c.Config != nil {
		if url := strings.TrimSpace(c.Config.AuthorityServiceURL); url != "" {
			return strings.TrimRight(url, "/"), nil
		}
	}

	return "", fmt.Errorf("authority service URL is required: set AuthorityServiceURL in wallet.conf")
}
