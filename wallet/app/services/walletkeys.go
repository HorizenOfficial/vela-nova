package services

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"

	"github.com/horizen-pes/pkg/crypto"
)

func GenerateKeys() (string, string) {
	//P521
	var key, _ = crypto.GeneratePrivateKeyP521()
	der, _ := x509.MarshalPKCS8PrivateKey(key.PrivateKey)
	base64Key := base64.StdEncoding.EncodeToString(der)
	fmt.Println(string(base64Key))

	//25519
	var key2, _ = crypto.GeneratePrivateKey25519()
	der2, _ := x509.MarshalPKCS8PrivateKey(key2.PrivateKey)
	base64Key2 := base64.StdEncoding.EncodeToString(der2)

	return string(base64Key), string(base64Key2)
}
