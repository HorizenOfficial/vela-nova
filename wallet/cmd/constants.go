package cmd

import (
	"github.com/horizen-pes/pkg/common"
)

var NOVA_APPLICATION_ID = common.NewApplicationId(1)

const PROTOCOL_VERSION uint8 = 0
const BLOCK_BATCH_SIZE = 100000
const BALANCE_JSON_KEY = "balance"