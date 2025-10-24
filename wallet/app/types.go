package app

import (
	"context"
	"fmt"
	"os"
	"time"

	ethCommon "github.com/ethereum/go-ethereum/common"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/common"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/magiconair/properties"
)

const ConfFileName = "wallet.conf"

type Config struct {
	KeyP521 cryptotypes.PrivateKeyP521
	KeySecp cryptotypes.PrivateKeySecp256k1
	RpcUrl string
	ProcessorEndpointAddress ethCommon.Address
	TeeAuthenticatorAddress ethCommon.Address
	// BlockchainPollingInterval is the interval at which to poll the blockchain for events
	BlockchainPollingInterval int64
	// BlockchainPollingTimeout is the max time interval at which to wait for events from the blockchain 
	BlockchainPollingTimeout int64
}

type AppCommand struct {
	Config Config
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
			Config: *config,
		}
	} 

	config, err := LoadConfigFromFile(ConfFileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while loading configuration from file %v", err)
        os.Exit(1)
	} 

	return &AppCommand{
		Config: *config,
	}
		
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

func LoadConfigFromFile(confFileName string) (*Config, error) {
	if !fileExists(confFileName) {
		return nil, fmt.Errorf("file conf not found")
	} else {
		// Load properties from file
		config, err := properties.LoadFile(confFileName, properties.UTF8)
		if err != nil {
			return nil, fmt.Errorf("error loading conf file %w", err)
		}
		keySecp, err := crypto.ImportPrivateKeySecp256k1FromHex(config.MustGetString("keySecp256k1"))
		if err != nil {
			return nil, fmt.Errorf("error importing secp256 key: %w", err)
		}
		keyP521, err := crypto.ImportPrivateKeyP521FromHex(config.MustGetString("keyP521"))
		if err != nil {
			return nil, fmt.Errorf("error importing P521 key: %w", err)
		}
		rpcUrl := config.MustGetString("rpcUrl")
		processorAddress := config.MustGetString("ProcessorAddress")
		if !ethCommon.IsHexAddress(processorAddress) {
			return nil, fmt.Errorf("processor address %s is not a valid hex address", processorAddress)
		}
		teeAuthenticatorAddress := config.MustGetString("TeeAuthenticatorAddress")
		if !ethCommon.IsHexAddress(teeAuthenticatorAddress) {
			return nil, fmt.Errorf("tee authenticator address %s is not a valid hex address", teeAuthenticatorAddress)
		}
		return &Config{
				KeySecp: *keySecp,
				KeyP521: *keyP521,
				RpcUrl: rpcUrl,
				ProcessorEndpointAddress: ethCommon.HexToAddress(processorAddress),
				TeeAuthenticatorAddress: ethCommon.HexToAddress(teeAuthenticatorAddress),
				BlockchainPollingInterval: config.GetInt64("BlockchainPollingInterval", 2),
				BlockchainPollingTimeout: config.GetInt64("BlockchainPollingTimeout", 60),
			}, nil
		
	}

}


type ChainCommand struct {
	*AppCommand
	BlockchainClient blockchain.Client
}

func NewChainCommand(config *Config, blockchainClient blockchain.Client) *ChainCommand {
	return  &ChainCommand{AppCommand: NewAppCommand(config), BlockchainClient: blockchainClient}
}


func (c *ChainCommand) InitChainClient() error {
	if c.BlockchainClient == nil {
		//create blockchain client
		c.BlockchainClient = blockchain.NewBlockChainClient(c.Config.ProcessorEndpointAddress, c.Config.TeeAuthenticatorAddress, c.Config.RpcUrl, &c.Config.KeySecp)
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

func (c *ChainCommand) WaitForRequestCompleted(requestID string, blockNumber uint64) (bool, error) {

	ticker := time.NewTicker(time.Duration(c.Config.BlockchainPollingInterval) * time.Second)
	defer ticker.Stop()

	timeoutCh := time.After(time.Duration(c.Config.BlockchainPollingTimeout) * time.Second)

	toBlock := blockNumber + 1
	for {
		select {
		case <-ticker.C:
			result, err := c.BlockchainClient.GetRequestCompletedEvent(context.Background(), requestID, 0, toBlock)
			if err != nil {
				fmt.Printf("Error getting request completion event: %v. Retrying", err)
				continue
			}
			if result == nil {
				fmt.Println("Waiting for confirmation from PES...")
				continue
			}
			if result.Status != common.RequestResultOK {
				fmt.Println("Deposit failed")
				return false, nil
			}
			fmt.Println("Deposit completed successfully")
			return true, nil

		case <-timeoutCh:
			fmt.Println("Timeout expired while waiting for confirmation from PES")
			return false, fmt.Errorf("timeout expired while waiting for confirmation from PES")
		}
	}
	
}