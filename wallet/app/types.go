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
}

type AppCommand struct {
	Config Config
}

func NewAppCommandNoConfig() *AppCommand {
	return &AppCommand{}
}

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
			return &AppCommand{
				Config: Config{
					KeySecp: *keySecp,
					KeyP521: *keyP521,
				},
			}
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}
