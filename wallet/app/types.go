package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/HorizenOfficial/vela-common-go/subgraph"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/magiconair/properties"
)

const ConfFileName = "wallet.conf"

type Config struct {
	KeyP521                  *cryptotypes.PrivateKeyP521
	KeySecp                  *cryptotypes.PrivateKeySecp256k1
	RpcUrl                   string
	ProcessorEndpointAddress *ethCommon.Address
	TeeAuthenticatorAddress  *ethCommon.Address
	AuthorityServiceURL      string
	SubgraphURL              string
	// ApplicationID is the on-chain application identifier assigned during deploy
	ApplicationID common.ApplicationIdType
	// BlockchainPollingInterval is the interval at which to poll the blockchain for events
	BlockchainPollingInterval int64
	// BlockchainPollingTimeout is the max time interval at which to wait for events from the blockchain
	BlockchainPollingTimeout int64
}

type AppCommand struct {
	Config *Config
}

/*
Constructor function for commands that do not require configuration
*/
func NewAppCommandNoConfig() *AppCommand {
	return &AppCommand{}
}

/*
Constructor function for commands that require configuration: if called with
no config parameter (default use-case) the config will be loaded from a loaded conf file.
Otherwise an explicit config can be passed (for example to execute unit tests)
*/
func NewAppCommand(config *Config) *AppCommand {
	if config != nil {
		return &AppCommand{
			Config: config,
		}
	}

	config, err := LoadConfigFromFile(ConfFileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while loading configuration from file '%s': %v\n", ConfFileName, err)
		os.Exit(1)
	}

	return &AppCommand{
		Config: config,
	}

}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// SaveApplicationID updates the ApplicationID in the wallet.conf file.
// If an existing ApplicationID line is found, it is commented out with a
// timestamp and the new value is written below it. If no existing line is
// found, the new value is appended to the file.
func SaveApplicationID(confFile string, appID common.ApplicationIdType) error {
	data, err := os.ReadFile(confFile)
	if err != nil {
		return fmt.Errorf("reading %s: %w", confFile, err)
	}

	newValue := fmt.Sprintf("ApplicationID=%d", appID)
	timestamp := time.Now().Format("2006-01-02")

	var out []string
	found := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if !found && strings.HasPrefix(trimmed, "ApplicationID=") && !strings.HasPrefix(trimmed, "#") {
			oldValue := strings.TrimPrefix(trimmed, "ApplicationID=")
			if oldValue != "" && oldValue != strconv.FormatUint(uint64(appID), 10) {
				out = append(out, fmt.Sprintf("# Previous ApplicationID (replaced by deployapp on %s)", timestamp))
				out = append(out, "# "+trimmed)
			}
			out = append(out, newValue)
			found = true
		} else {
			out = append(out, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning %s: %w", confFile, err)
	}

	if !found {
		out = append(out, newValue)
	}

	// Ensure file ends with newline
	content := strings.Join(out, "\n") + "\n"
	return os.WriteFile(confFile, []byte(content), 0o644)
}

// SaveConfigToFile writes a Config to a properties file compatible with LoadConfigFromFile.
func SaveConfigToFile(cfg *Config, path string) error {
	var lines []string

	keyP521Hex := ""
	if cfg.KeyP521 != nil {
		keyP521Hex = crypto.ExportPrivateKeyP521ToHex(cfg.KeyP521)
	}
	lines = append(lines, "keyP521="+keyP521Hex)

	keySecpHex := ""
	if cfg.KeySecp != nil {
		keySecpHex = crypto.ExportPrivateKeySecp256k1ToHex(cfg.KeySecp)
	}
	lines = append(lines, "keySecp256k1="+keySecpHex)

	lines = append(lines, "rpcUrl="+cfg.RpcUrl)

	processorAddr := ""
	if cfg.ProcessorEndpointAddress != nil {
		processorAddr = cfg.ProcessorEndpointAddress.Hex()
	}
	lines = append(lines, "ProcessorAddress="+processorAddr)

	teeAddr := ""
	if cfg.TeeAuthenticatorAddress != nil {
		teeAddr = cfg.TeeAuthenticatorAddress.Hex()
	}
	lines = append(lines, "TeeAuthenticatorAddress="+teeAddr)

	lines = append(lines, "AuthorityServiceURL="+cfg.AuthorityServiceURL)
	lines = append(lines, "SubgraphURL="+cfg.SubgraphURL)

	if cfg.ApplicationID != 0 {
		lines = append(lines, fmt.Sprintf("ApplicationID=%d", cfg.ApplicationID))
	} else {
		lines = append(lines, "ApplicationID=")
	}

	if cfg.BlockchainPollingInterval > 0 {
		lines = append(lines, fmt.Sprintf("BlockchainPollingInterval=%d", cfg.BlockchainPollingInterval))
	} else {
		lines = append(lines, "BlockchainPollingInterval=")
	}

	if cfg.BlockchainPollingTimeout > 0 {
		lines = append(lines, fmt.Sprintf("BlockchainPollingTimeout=%d", cfg.BlockchainPollingTimeout))
	} else {
		lines = append(lines, "BlockchainPollingTimeout=")
	}

	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func LoadConfigFromFile(confFileName string) (*Config, error) {
	if !fileExists(confFileName) {
		return nil, fmt.Errorf("config file %s not found", confFileName)
	} else {
		// Load properties from file
		config, err := properties.LoadFile(confFileName, properties.UTF8)
		if err != nil {
			return nil, fmt.Errorf("error loading conf file %w", err)
		}

		var keySecp *cryptotypes.PrivateKeySecp256k1
		if keySecpFromFile := config.MustGetString("keySecp256k1"); keySecpFromFile != "" {
			keySecp, err = crypto.ImportPrivateKeySecp256k1FromHex(keySecpFromFile)
			if err != nil {
				return nil, fmt.Errorf("error importing secp256 key: %w", err)
			}
		}

		var keyP521 *cryptotypes.PrivateKeyP521
		if keyP521FromFile := config.MustGetString("keyP521"); keyP521FromFile != "" {
			keyP521, err = crypto.ImportPrivateKeyP521FromHex(keyP521FromFile)
			if err != nil {
				return nil, fmt.Errorf("error importing P521 key: %w", err)
			}
		}
		rpcUrl := config.MustGetString("rpcUrl")
		authorityURL := config.GetString("AuthorityServiceURL", "")
		subgraphURL := config.GetString("SubgraphURL", "")
		if subgraphURL != "" {
			if _, err := url.ParseRequestURI(subgraphURL); err != nil {
				return nil, fmt.Errorf("subgraph url is not valid: %w", err)
			}
		}

		var processorEndpointAddress ethCommon.Address
		if processorAddress := config.MustGetString("ProcessorAddress"); processorAddress != "" {
			if !ethCommon.IsHexAddress(processorAddress) {
				return nil, fmt.Errorf("processor address %s is not a valid hex address", processorAddress)
			}
			processorEndpointAddress = ethCommon.HexToAddress(processorAddress)
		}

		var teeAuthenticatorAddress ethCommon.Address
		if teeAddress := config.MustGetString("TeeAuthenticatorAddress"); teeAddress != "" {
			if !ethCommon.IsHexAddress(teeAddress) {
				return nil, fmt.Errorf("tee authenticator address %s is not a valid hex address", teeAddress)
			}
			teeAuthenticatorAddress = ethCommon.HexToAddress(teeAddress)
		}

		var applicationID common.ApplicationIdType
		if appIDStr := config.GetString("ApplicationID", ""); appIDStr != "" {
			appIDVal, err := strconv.ParseUint(appIDStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid ApplicationID %q: %w", appIDStr, err)
			}
			applicationID = common.NewApplicationId(appIDVal)
		}

		return &Config{
			KeySecp:                   keySecp,
			KeyP521:                   keyP521,
			RpcUrl:                    rpcUrl,
			ProcessorEndpointAddress:  &processorEndpointAddress,
			TeeAuthenticatorAddress:   &teeAuthenticatorAddress,
			AuthorityServiceURL:       authorityURL,
			SubgraphURL:               subgraphURL,
			ApplicationID:             applicationID,
			BlockchainPollingInterval: config.GetInt64("BlockchainPollingInterval", 2),
			BlockchainPollingTimeout:  config.GetInt64("BlockchainPollingTimeout", 60),
		}, nil

	}

}

type ChainCommand struct {
	*AppCommand
	BlockchainClient blockchain.Client
	SubgraphClient   subgraph.Client
}

func NewChainCommand(config *Config, blockchainClient blockchain.Client) *ChainCommand {
	appCmd := NewAppCommand(config)
	var sgClient subgraph.Client
	if appCmd.Config != nil && appCmd.Config.SubgraphURL != "" {
		sgClient = subgraph.NewClient(appCmd.Config.SubgraphURL)
	}
	return &ChainCommand{
		AppCommand:       appCmd,
		BlockchainClient: blockchainClient,
		SubgraphClient:   sgClient,
	}
}

func (c *ChainCommand) InitChainClient(ctx context.Context) error {
	if c.BlockchainClient == nil {
		//create blockchain client
		// Check that required config fields are set
		if c.Config.ProcessorEndpointAddress == nil || c.Config.TeeAuthenticatorAddress == nil || c.Config.KeySecp == nil || c.Config.RpcUrl == "" {
			return fmt.Errorf("missing required configuration fields to create blockchain client")
		}
		c.BlockchainClient = blockchain.NewBlockChainClient(*c.Config.ProcessorEndpointAddress, *c.Config.TeeAuthenticatorAddress, c.Config.RpcUrl, c.Config.KeySecp)
		err := c.BlockchainClient.Connect(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *ChainCommand) CloseClient() error {
	if c.BlockchainClient == nil {
		return fmt.Errorf("client not initialized")
	}
	return c.BlockchainClient.Close()
}

func (c *ChainCommand) WaitForRequestCompleted(requestID common.RequestIdType, ctx context.Context) error {

	ticker := time.NewTicker(time.Duration(c.Config.BlockchainPollingInterval) * time.Second)
	defer ticker.Stop()

	timeoutCh := time.After(time.Duration(c.Config.BlockchainPollingTimeout) * time.Second)

	for {
		select {
		case <-ticker.C:
			if c.SubgraphClient == nil {
				return fmt.Errorf("subgraph client not initialized")
			}
			result, err := c.SubgraphClient.GetRequestCompletedByID(ctx, requestID)
			if err != nil {
				fmt.Printf("Error getting request completion event: %v. Retrying\n", err)
				continue
			}
			if result == nil {
				fmt.Println("Waiting for confirmation from Vela...")
				continue
			}
			if result.Status != common.RequestResultOK {
				failureMsg := result.ErrorMessage
				if failureMsg == "" {
					failureMsg = "request failed"
				}
				return fmt.Errorf("%s (code %d)", failureMsg, result.ErrorCode)
			}
			fmt.Println("Request completed successfully")
			return nil

		case <-timeoutCh:
			fmt.Println("Timeout expired while waiting for confirmation from Vela")
			return fmt.Errorf("timeout expired while waiting for confirmation from Vela")
		}
	}

}

func (c *ChainCommand) WaitForDeployRequestCompleted(requestID common.RequestIdType, ctx context.Context) error {

	ticker := time.NewTicker(time.Duration(c.Config.BlockchainPollingInterval) * time.Second)
	defer ticker.Stop()

	timeoutCh := time.After(time.Duration(c.Config.BlockchainPollingTimeout) * time.Second)

	for {
		select {
		case <-ticker.C:
			if c.SubgraphClient == nil {
				return fmt.Errorf("subgraph client not initialized")
			}
			result, err := c.SubgraphClient.GetDeployRequestCompletedByID(ctx, requestID)
			if err != nil {
				fmt.Printf("Error getting deploy request completion event: %v. Retrying\n", err)
				continue
			}
			if result == nil {
				fmt.Println("Waiting for deploy confirmation from Vela...")
				continue
			}
			if result.Status != common.RequestResultOK {
				failureMsg := result.ErrorMessage
				if failureMsg == "" {
					failureMsg = "deploy request failed"
				}
				return fmt.Errorf("%s (code %d)", failureMsg, result.ErrorCode)
			}
			fmt.Println("Deploy request completed successfully")
			return nil

		case <-timeoutCh:
			fmt.Println("Timeout expired while waiting for deploy confirmation from Vela")
			return fmt.Errorf("timeout expired while waiting for deploy confirmation from Vela")
		}
	}

}

// EncryptPayload encrypts the given payload using ECIES with the TEE public key retrieved from the blockchain client.
// Returns the encrypted payload bytes or an error if marshalling, key retrieval, or encryption fails.
func (c *ChainCommand) EncryptPayload(payload any, ctx context.Context) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error preparing process payload: %w", err)
	}
	receiverPubKey, err := c.BlockchainClient.GetTeePublicKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("error retrieving Vela public key: %w", err)
	}
	encryptedPayload, err := crypto.Encrypt(c.Config.KeyP521, receiverPubKey, payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("error encrypting process payload: %w", err)
	}
	return encryptedPayload, nil
}
