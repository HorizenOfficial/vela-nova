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

func TestGetPrivateBalance(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	//prepare args
	args := []string{"1"}
	mockEvent := []byte(`{"balance": "12345"}`)
	// Execute the command
	cmd := NewGetPrivateBalanceCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
		RpcUrl: "https://base-sepolia.drpc.org",
	}, mockEvent).Command()
	cmd.Run(nil, args)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	
	assert.Contains(t, output, "12345")
}