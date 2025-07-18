package app

import (
	"github.com/horizen-pes/pkg/common"
)

type AppContext struct {
	KeyP521 common.PrivateKeyP521
	KeySecp common.PrivateKeySecp256k1
}
