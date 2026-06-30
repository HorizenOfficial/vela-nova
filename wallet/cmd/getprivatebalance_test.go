package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/HorizenOfficial/vela-common-go/subtypes"
	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela-nova/wallet/cmd/testutil"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/HorizenOfficial/vela-common-go/subgraph"
	"github.com/magiconair/properties"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testAppID = common.NewApplicationId(1)

// Test blockchain client that returns a fixed TEE public key.
type TestGetPrivateBalanceBlockChainClient struct {
	*blockchain.MockClient
	teePub *cryptotypes.PublicKeyP521
}

func (c *TestGetPrivateBalanceBlockChainClient) GetTeePublicKey(ctx context.Context) (*cryptotypes.PublicKeyP521, error) {
	return c.teePub, nil
}

func TestGetPrivateBalance_HexEncodedBalance(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	seed, _ := GenerateSeed(key1)
	subtypesList := EventSubTypesFromSeed(seed, subtypes.DefaultSubtypeN)

	// Balance must be hex-encoded with 0x prefix for common.Big unmarshaling
	// 12345 decimal = 0x3039 hex
	mockEvent := []byte(`{"balance": "0x3039"}`)
	encrypted, _ := crypto.Encrypt(teeKey, key2.PublicKey(), mockEvent)

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	sgClient := subgraph.NewMockClient().WithUserEvents(testAppID, []subgraph.UserEvent{
		{
			ApplicationID: testAppID,
			EncryptedData: encrypted,
			EventSubType:  subtypesList[0],
		},
	})
	// Execute the command
	cfg := &app.Config{
		KeySecp:       key1,
		KeyP521:       key2,
		ApplicationID: testAppID,
	}
	testutil.WriteTempConf(t, cfg)
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(cfg, client)
	getPrivateBalanceCmd.SubgraphClient = sgClient
	cmd := getPrivateBalanceCmd.Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "0.000000000000012345")
}

func TestGetPrivateBalance_NoEventsReturnsZero(t *testing.T) {
	// This test verifies that when no events are found, the default zero balance
	// is returned and properly unmarshaled. 
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	// Empty events - no user events returned from subgraph
	sgClient := subgraph.NewMockClient().WithUserEvents(testAppID, []subgraph.UserEvent{})

	cfg := &app.Config{
		KeySecp:       key1,
		KeyP521:       key2,
		ApplicationID: testAppID,
	}
	testutil.WriteTempConf(t, cfg)
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(cfg, client)
	getPrivateBalanceCmd.SubgraphClient = sgClient
	cmd := getPrivateBalanceCmd.Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "No ETH balance found")
}

func TestGetPrivateBalance_InvalidEventFilteredOut(t *testing.T) {
	// When an event doesn't pass the filter (no Balance key), we fall back to zero
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	teePub := teeKey.PublicKey()

	// Event without Balance key - will be filtered out
	mockEvent := []byte(`{"other_field": "value"}`)
	encrypted, _ := crypto.Encrypt(teeKey, key2.PublicKey(), mockEvent)

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teePub,
	}

	sgClient := subgraph.NewMockClient().WithUserEvents(testAppID, []subgraph.UserEvent{
		{
			ApplicationID: testAppID,
			EncryptedData: encrypted,
		},
	})

	cfg := &app.Config{
		KeySecp:       key1,
		KeyP521:       key2,
		ApplicationID: testAppID,
	}
	testutil.WriteTempConf(t, cfg)
	getPrivateBalanceCmd := NewGetPrivateBalanceCommand(cfg, client)
	getPrivateBalanceCmd.SubgraphClient = sgClient
	cmd := getPrivateBalanceCmd.Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "No ETH balance found")
}

// ---------- ERC-20 (USDC) coverage for findTokenBalance ----------

// testUSDCAddrHex is a synthetic test-only token address used for the USDC
// coverage below. The CAFEBABE prefix makes intent unambiguous.
const testUSDCAddrHex = "0xcafeBABE00000000000000000000000000000001"

// ethZeroAddrHex is the ETH (zero-address) hex form, as the WASM module emits
// it in deposit/transfer/withdraw events.
const ethZeroAddrHex = "0x0000000000000000000000000000000000000000"

// makeUSDCRegistry returns a TokenRegistry with USDC (6 decimals) registered.
func makeUSDCRegistry(t *testing.T) *app.TokenRegistry {
	t.Helper()
	p, err := properties.LoadString("token.USDC.address=" + testUSDCAddrHex + "\n" +
		"token.USDC.decimals=6\n")
	require.NoError(t, err)
	r, err := app.LoadTokenRegistry(p)
	require.NoError(t, err)
	return r
}

// privateBalanceCase bundles the inputs needed to drive a getprivatebalance
// invocation: the JSON event payloads to seed into the mock subgraph (in
// chronological order) and the value of the --token flag ("" = default ETH).
type privateBalanceCase struct {
	events []string
	token  string
}

// run executes the case end-to-end: generates fresh keys, encrypts each event
// from a simulated TEE to the user, wires up a mock subgraph + blockchain
// client, runs `getprivatebalance` with the requested --token, and returns
// captured stdout.
func (c privateBalanceCase) run(t *testing.T, tokens *app.TokenRegistry) string {
	t.Helper()

	userSecp, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	teeKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	seed, err := GenerateSeed(userSecp)
	require.NoError(t, err)
	subtypesList := EventSubTypesFromSeed(seed, subtypes.DefaultSubtypeN)

	var sgEvents []subgraph.UserEvent
	for _, payload := range c.events {
		enc, err := crypto.Encrypt(teeKey, userKey.PublicKey(), []byte(payload))
		require.NoError(t, err)
		sgEvents = append(sgEvents, subgraph.UserEvent{
			ApplicationID: testAppID,
			EncryptedData: enc,
			EventSubType:  subtypesList[0],
		})
	}
	sg := subgraph.NewMockClient().WithUserEvents(testAppID, sgEvents)

	client := &TestGetPrivateBalanceBlockChainClient{
		MockClient: blockchain.NewMockClient(),
		teePub:     teeKey.PublicKey(),
	}

	cfg := &app.Config{
		KeySecp:       userSecp,
		KeyP521:       userKey,
		ApplicationID: testAppID,
		Tokens:        tokens,
		// RpcUrl deliberately left empty: the blockchain client is injected
		// below, so InitChainClient (the only consumer of RpcUrl) is bypassed.
	}
	testutil.WriteTempConf(t, cfg)

	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	cmd := NewGetPrivateBalanceCommand(cfg, client)
	cmd.SubgraphClient = sg
	cobraCmd := cmd.Command()
	if c.token != "" {
		require.NoError(t, cobraCmd.Flags().Set("token", c.token))
	}
	cobraCmd.Run(cobraCmd, nil)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// TestGetPrivateBalance_USDC_FormatsWithCorrectDecimals exercises both the
// per-token filter and the decimals-aware formatting path: a single USDC
// balance event of 100_000_000 raw units (= 100 USDC at 6 decimals) must be
// rendered as "100 USDC" — *not* the 18-decimal ETH formatting that would
// produce a near-zero string.
func TestGetPrivateBalance_USDC_FormatsWithCorrectDecimals(t *testing.T) {
	out := privateBalanceCase{
		// 100 USDC = 100_000_000 raw units = 0x5F5E100.
		events: []string{
			`{"tokenAddress":"` + strings.ToLower(testUSDCAddrHex) + `","balance":"0x5f5e100"}`,
		},
		token: "USDC",
	}.run(t, makeUSDCRegistry(t))

	assert.Contains(t, out, "100 USDC")
}

// TestGetPrivateBalance_USDC_FilteredFromMixedStream verifies that the
// per-token filter actually skips ETH events when USDC is requested. The ETH
// event is placed FIRST in the stream so the only way the test can return
// "100 USDC" is for findTokenBalance to recognize the tokenAddress mismatch
// and continue scanning to the USDC event behind it.
func TestGetPrivateBalance_USDC_FilteredFromMixedStream(t *testing.T) {
	out := privateBalanceCase{
		events: []string{
			// ETH event with explicit zero tokenAddress (current WASM emits this
			// shape for ETH deposits/transfers/withdrawals).
			`{"tokenAddress":"` + ethZeroAddrHex + `","balance":"0x3039"}`,
			// USDC event further back in the stream.
			`{"tokenAddress":"` + strings.ToLower(testUSDCAddrHex) + `","balance":"0x5f5e100"}`,
		},
		token: "USDC",
	}.run(t, makeUSDCRegistry(t))

	assert.Contains(t, out, "100 USDC")
	// Sanity: the ETH balance (12345 raw / 6 decimals = 0.012345 USDC) must NOT
	// have leaked through as if it were a USDC value.
	assert.NotContains(t, out, "0.012345")
}

// TestGetPrivateBalance_USDC_OnlyETHEventsReturnsNoBalance ensures the filter
// never falls back to ETH when USDC is requested: even if the user has an
// active ETH balance, asking for USDC must report "No USDC balance found" so
// the user is not misled into thinking they hold USDC.
func TestGetPrivateBalance_USDC_OnlyETHEventsReturnsNoBalance(t *testing.T) {
	out := privateBalanceCase{
		events: []string{
			`{"tokenAddress":"` + ethZeroAddrHex + `","balance":"0x3039"}`,
		},
		token: "USDC",
	}.run(t, makeUSDCRegistry(t))

	assert.Contains(t, out, "No USDC balance found")
}

// TestGetPrivateBalance_USDC_CaseInsensitiveTokenAddressMatch pins the
// EqualFold path in findTokenBalance: the on-chain emitter may produce the
// tokenAddress in EIP-55 mixed case, while the wallet derives a lowercased
// hex for comparison. The two forms must still be recognized as the same
// token.
func TestGetPrivateBalance_USDC_CaseInsensitiveTokenAddressMatch(t *testing.T) {
	// All-caps form (after the 0x) — distinctly different casing from the
	// lowercased filter value the wallet computes internally.
	upperHex := "0x" + strings.ToUpper(testUSDCAddrHex[2:])

	out := privateBalanceCase{
		events: []string{
			`{"tokenAddress":"` + upperHex + `","balance":"0x5f5e100"}`,
		},
		token: "USDC",
	}.run(t, makeUSDCRegistry(t))

	assert.Contains(t, out, "100 USDC")
}
