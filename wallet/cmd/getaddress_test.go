package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/horizen-pes-nova/wallet/app"
	"github.com/horizen-pes/pkg/crypto"
	"github.com/stretchr/testify/assert"
)

func TestGetAddressCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key, _ = crypto.GeneratePrivateKeySecp256k1()
	expectedAddress := key.PublicKey().Address()

	// Execute the command
	cmd := NewGetAddressCommand(&app.Config{
		KeySecp: key,
	}).Command()
	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, expectedAddress)
}
