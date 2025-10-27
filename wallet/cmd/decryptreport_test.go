package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/blockchain"
	"github.com/horizen-pes/pkg/crypto"
	cryptotypes "github.com/horizen-pes/pkg/common/crypto"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"

)

// create a test blockchain client with only the GetTeePublicKey method defined
type TestDecryptReportBlockChainClient struct {
	blockchain.MockClient
	key *cryptotypes.PublicKeyP521
}

///rewrite SubmitRequest
func (c *TestDecryptReportBlockChainClient) GetTeePublicKey(ctx context.Context) (*cryptotypes.PublicKeyP521, error) {
	return c.key, nil
}

func TestDecryptReport(t *testing.T) {
	// create keys
	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	teeKey, _ := crypto.GeneratePrivateKeyP521()
	// generate encrypted payload
	payload := "test report 12345"
	encrypted, err := crypto.Encrypt(teeKey, key2.PublicKey(), []byte(payload))
	require.NoError(t, err)
	// write to file
	filePath := "/tmp/testReport"
	err = os.WriteFile(filePath, encrypted, 0644)
	require.NoError(t, err)

	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	//create client

	client := &TestDecryptReportBlockChainClient{
		*blockchain.NewMockClient(),
		teeKey.PublicKey(),
	}
	args := []string{filePath}

	// Execute the command
	cmd := NewDecryptReportCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
	}, client).Command()
	cmd.Run(nil, args)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, payload)
}
