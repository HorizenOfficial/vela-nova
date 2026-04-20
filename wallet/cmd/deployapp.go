package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	velacommon "github.com/HorizenOfficial/vela-common-go/common"
	runtimeapp "github.com/HorizenOfficial/vela-nova/payment-app/app"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
)

type DeployAppCommand struct {
	*app.ChainCommand
	maxFeeValue   string
	wasmPath      string
	confFile      string // path to wallet.conf for persisting ApplicationID
	allowedTokens []string
}

func NewDeployAppCommand(config *app.Config, blockchainClient blockchain.Client, confFile string) *DeployAppCommand {
	return &DeployAppCommand{
		ChainCommand: app.NewChainCommand(config, blockchainClient),
		confFile:     confFile,
	}
}

func (c *DeployAppCommand) Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployapp",
		Short: `Uploads a wasm artifact and triggers app deployment`,
		Long:  `Uploads a wasm artifact to the authority service and submits a deploy request with an artifact descriptor payload.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.Exec(context.Background()); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		},
	}
	cmd.Flags().StringVarP(&c.maxFeeValue, "max-value-fee", "f", "100 wei", "Maximum fee value reserved for this request (e.g., 0.1 ETH)")
	cmd.Flags().StringVar(&c.wasmPath, "wasm", "", "Path to wasm module file to upload and deploy")
	cmd.Flags().StringSliceVar(&c.allowedTokens, "allowed-tokens", nil,
		"Comma-separated ERC-20 symbols or hex addresses to allowlist at deploy (ETH is always allowed). "+
			"Symbols are resolved via the wallet.conf token registry.")
	_ = cmd.MarkFlagRequired("wasm")
	return cmd
}

// Exec runs the deploy flow outside of the Cobra wrapper. Exported so test
// drivers (wallet/testutil) can invoke it directly after setting flag-bound
// struct fields via cmd.Flags().Set(...).
func (c *DeployAppCommand) Exec(ctx context.Context) error {
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

	uploadResp, err := uploadWASM(ctx, serviceURL, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to upload wasm artifact: %w", err)
	}
	if uploadResp.WasmSHA256 != localSHA {
		return fmt.Errorf("deploy upload hash mismatch: local=%s remote=%s", localSHA, uploadResp.WasmSHA256)
	}
	if uploadResp.ArtifactID != expectedArtifactID {
		return fmt.Errorf("deploy upload artifactId mismatch: expected=%s remote=%s", expectedArtifactID, uploadResp.ArtifactID)
	}

	// Resolve --allowed-tokens to lowercase hex addresses and build ConstructorParams.
	// The guest keys AllowedTokens by the exact hex string; the runtime looks up using
	// types.Address.Hex() which produces lowercase hex, so we normalize to match.
	var ctorParams velacommon.ConstructorParams
	if len(c.allowedTokens) > 0 {
		resolved := make([]string, 0, len(c.allowedTokens))
		seen := make(map[string]bool)
		for _, tok := range c.allowedTokens {
			info, err := c.Config.Tokens.ResolveToken(tok)
			if err != nil {
				return fmt.Errorf("--allowed-tokens: %w", err)
			}
			if info.Address == (ethCommon.Address{}) {
				// ETH is always allowed by the guest — don't include it explicitly
				continue
			}
			addrHex := strings.ToLower(info.Address.Hex())
			if seen[addrHex] {
				continue
			}
			seen[addrHex] = true
			resolved = append(resolved, addrHex)
		}
		if len(resolved) > 0 {
			params := runtimeapp.DeployParams{AllowedTokens: resolved}
			ctorParams, err = json.Marshal(params)
			if err != nil {
				return fmt.Errorf("failed to marshal constructor params: %w", err)
			}
		}
	}

	deployPayload, err := json.Marshal(velacommon.DeployDescriptor{
		Mode:              velacommon.DeployModeArtifactRef,
		ArtifactID:        uploadResp.ArtifactID,
		WasmSHA256:        uploadResp.WasmSHA256,
		ConstructorParams: ctorParams,
	})
	if err != nil {
		return fmt.Errorf("failed to encode deploy descriptor payload: %w", err)
	}

	if err := c.InitChainClient(ctx); err != nil {
		return fmt.Errorf("error connecting to rpc node: %w", err)
	}
	defer c.BlockchainClient.Close()

	appID, requestID, _, err := c.BlockchainClient.SubmitDeployRequest(ctx, PROTOCOL_VERSION, deployPayload, maxFeeValue)
	if err != nil {
		return fmt.Errorf("error sending request to deploy app: %w", err)
	}

	fmt.Printf("Deploy request submitted. Assigned ApplicationID: %d\n", appID)
	fmt.Println("Waiting for deploy confirmation from Vela")
	waitErr := c.WaitForDeployRequestCompleted(requestID, ctx)

	if waitErr != nil && !errors.Is(waitErr, app.ErrPollingTimeout) {
		// Explicit failure from the executor (e.g., invalid WASM, already deployed).
		// The application was NOT registered on-chain — do not persist the ApplicationID.
		return fmt.Errorf("deploy app failed: %w", waitErr)
	}

	// Persist the ApplicationID on success OR timeout.
	//
	// On timeout, the deploy may have succeeded on-chain but the subgraph confirmation
	// was not received in time. We save the ID so the user does not lose it.
	//
	// If the deploy actually failed on-chain despite the timeout (rare: requires the
	// subgraph to be down while the executor rejects the request), the user will see
	// an "invalid application ID" error on the next command. Recovery options:
	//   1. Re-run "deployapp" — it uploads the WASM again, gets a fresh appID, and
	//      overwrites the stale value in wallet.conf (old value is commented out).
	//   2. Manually check the subgraph or contract to verify registration, then
	//      edit wallet.conf if needed.
	if err := app.SaveApplicationID(c.confFile, appID); err != nil {
		return fmt.Errorf("failed to save ApplicationID to %s: %w", c.confFile, err)
	}
	fmt.Printf("ApplicationID=%d saved to %s\n", appID, c.confFile)

	if waitErr != nil {
		// Timeout path — ID is saved but confirmation was not received.
		return fmt.Errorf("deploy confirmation timed out (ApplicationID %d saved to %s): %w", appID, c.confFile, waitErr)
	}

	fmt.Printf("Deploy app completed successfully. ApplicationID: %d\n", appID)
	return nil
}

type deployUploadResponse struct {
	ArtifactID string `json:"artifactId"`
	WasmSHA256 string `json:"wasmSha256"`
}

func uploadWASM(ctx context.Context, baseURL string, wasm []byte) (*deployUploadResponse, error) {
	if len(wasm) == 0 {
		return nil, fmt.Errorf("wasm payload is empty")
	}

	baseURL = strings.TrimRight(baseURL, "/")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fileWriter, err := writer.CreateFormFile("wasm", "app.wasm")
	if err != nil {
		return nil, fmt.Errorf("creating multipart payload: %w", err)
	}
	if _, err := fileWriter.Write(wasm); err != nil {
		return nil, fmt.Errorf("writing multipart payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/deploy/upload", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload wasm: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("deploy upload failed: %s", bytes.TrimSpace(body))
	}

	var out deployUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding deploy upload response: %w", err)
	}
	return &out, nil
}

func (c *DeployAppCommand) resolveArtifactServiceURL() (string, error) {
	if c.Config != nil {
		if url := strings.TrimSpace(c.Config.AuthorityServiceURL); url != "" {
			return strings.TrimRight(url, "/"), nil
		}
	}

	return "", fmt.Errorf("authority service URL is required: set AuthorityServiceURL in wallet.conf")
}
