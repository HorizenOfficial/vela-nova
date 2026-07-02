package cmd

import (
	"testing"

	pestestutil "github.com/HorizenOfficial/vela/pkg/blockchain/testutil"
)

// setupSimTestHelper builds a SimTestHelper with the defaults every wallet/cmd
// test wants: auto-mining on, mock TEE contracts, no pre-set tee signer. Pass
// teeKey.PublicKey().Bytes() when the test exercises a code path that needs a
// real secp521r1 pubkey on-chain; otherwise pass nil.
func setupSimTestHelper(t *testing.T, teePubSecp521r1 []byte) *pestestutil.SimTestHelper {
	return pestestutil.NewSimTestHelper(t, true, true, nil, teePubSecp521r1)
}
