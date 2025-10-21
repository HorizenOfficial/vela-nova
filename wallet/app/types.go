package app

import (
	"fmt"
	"os"

	ethCommon "github.com/ethereum/go-ethereum/common"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/magiconair/properties"
)

const confFileName = "wallet.conf"

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
	} else {
		if !fileExists(confFileName) {
			panic("File conf not found. Please create it and restart.")
		} else {
			// Load properties from file
			config, err := properties.LoadFile(confFileName, properties.UTF8)
			if err != nil {
				panic(err)
			}
			keySecp, _ := crypto.ImportPrivateKeySecp256k1FromHex(config.MustGetString("keySecp256k1"))
			keyP521, _ := crypto.ImportPrivateKeyP521FromHex(config.MustGetString("keyP521"))
			rpcUrl := config.MustGetString("rpcUrl")
			processorAddress := config.MustGetString("ProcessorAddress")
			if !ethCommon.IsHexAddress(processorAddress) {
				panic(fmt.Sprintf("processor address %s is not a valid hex address", processorAddress))
			}
			teeAuthenticatorAddress := config.MustGetString("TeeAuthenticatorAddress")
			if !ethCommon.IsHexAddress(teeAuthenticatorAddress) {
				panic(fmt.Sprintf("tee authenticator address %s is not a valid hex address", teeAuthenticatorAddress))
			}
			return &AppCommand{
				Config: Config{
					KeySecp: *keySecp,
					KeyP521: *keyP521,
					RpcUrl: rpcUrl,
					ProcessorEndpointAddress: ethCommon.HexToAddress(processorAddress),
					TeeAuthenticatorAddress: ethCommon.HexToAddress(teeAuthenticatorAddress),
					BlockchainPollingInterval: config.GetInt64("BlockchainPollingInterval", 2),
					BlockchainPollingTimeout: config.GetInt64("BlockchainPollingTimeout", 60),
				},
			}
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}
