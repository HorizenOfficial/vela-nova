package services

import (
	"github.com/horizen-pes/pkg/crypto"
)

func GenerateKeys() (string, string) {

	//secp
	var key, _ = crypto.GeneratePrivateKeySecp256k1()
	hexKeySecp := crypto.ExportPrivateKeySecp256k1ToHex(key)

	//P521
	var key2, _ = crypto.GeneratePrivateKeyP521()
	hexKeyP521 := crypto.ExportPrivateKeyP521ToHex(key2)

	return hexKeyP521, hexKeySecp
}
