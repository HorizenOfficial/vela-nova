package app

import (
	"os"

	"github.com/horizen-pes/pkg/common"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/magiconair/properties"
)

const confFileName = "wallet.conf"

type Config struct {
	KeyP521 common.PrivateKeyP521
	KeySecp common.PrivateKeySecp256k1
	RpcUrl string
	KeyRegistryAddress string
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
			keyRegistryAddress := config.MustGetString("keyRegistryAddress")
			return &AppCommand{
				Config: Config{
					KeySecp: *keySecp,
					KeyP521: *keyP521,
					RpcUrl: rpcUrl,
					KeyRegistryAddress: keyRegistryAddress,
				},
			}
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}
