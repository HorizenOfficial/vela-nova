package app

import (
	"github.com/horizen-pes/pkg/common"
)

type AppContext struct {
	keyP521  common.PrivateKeyP521
	key25519 common.PrivateKey25519
}
