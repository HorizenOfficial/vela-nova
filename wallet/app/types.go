package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/horizen-pes/pkg/subgraph"
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

		return &Config{
			KeySecp:                   keySecp,
			KeyP521:                   keyP521,
			RpcUrl:                    rpcUrl,
			ProcessorEndpointAddress:  &processorEndpointAddress,
			TeeAuthenticatorAddress:   &teeAuthenticatorAddress,
			AuthorityServiceURL:       authorityURL,
			SubgraphURL:               subgraphURL,
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

func (c *ChainCommand) InitChainClient() error {
	if c.BlockchainClient == nil {
		//create blockchain client
		// Check that required config fields are set
		if c.Config.ProcessorEndpointAddress == nil || c.Config.TeeAuthenticatorAddress == nil || c.Config.KeySecp == nil || c.Config.RpcUrl == "" {
			return fmt.Errorf("missing required configuration fields to create blockchain client")
		}
		c.BlockchainClient = blockchain.NewBlockChainClient(*c.Config.ProcessorEndpointAddress, *c.Config.TeeAuthenticatorAddress, c.Config.RpcUrl, c.Config.KeySecp)
		err := c.BlockchainClient.Connect(context.Background())
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

func (c *ChainCommand) WaitForRequestCompleted(requestID common.RequestIdType, blockNumber uint64, ctx context.Context) error {

	ticker := time.NewTicker(time.Duration(c.Config.BlockchainPollingInterval) * time.Second)
	defer ticker.Stop()

	timeoutCh := time.After(time.Duration(c.Config.BlockchainPollingTimeout) * time.Second)

	_ = blockNumber
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
				fmt.Println("Waiting for confirmation from PES...")
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
			fmt.Println("Timeout expired while waiting for confirmation from PES")
			return fmt.Errorf("timeout expired while waiting for confirmation from PES")
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
		return nil, fmt.Errorf("error retrieving PES public key: %w", err)
	}
	encryptedPayload, err := crypto.Encrypt(c.Config.KeyP521, receiverPubKey, payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("error encrypting process payload: %w", err)
	}
	return encryptedPayload, nil
}
