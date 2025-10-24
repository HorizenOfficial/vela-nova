package cmd
import (
	"math/big"
)

var NOVA_APPLICATION_ID = *big.NewInt(1)
const PROTOCOL_VERSION uint8 = 0
const BLOCK_BATCH_SIZE = 100
const BALANCE_JSON_KEY = "balance"