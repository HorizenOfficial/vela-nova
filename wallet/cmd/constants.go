package cmd

import (
	ethCommon "github.com/ethereum/go-ethereum/common"
)

// ETH_TOKEN is the sentinel address representing native ETH.
// Matches Structs.sol: address constant ETH_TOKEN = address(0).
var ETH_TOKEN = ethCommon.Address{}

const PROTOCOL_VERSION uint8 = 0
const BLOCK_BATCH_SIZE = 100000
