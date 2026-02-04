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

func TestGetPublicBalanceCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key, _ = crypto.GeneratePrivateKeySecp256k1()

	// Execute the command
	cmd := NewGetPublicBalanceCommand(&app.Config{
		KeySecp: key,
		RpcUrl: "https://base-sepolia.drpc.org",
	}).Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, "0.000000000000000000")
}
