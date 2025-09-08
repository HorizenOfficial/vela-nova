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

func TestRegisterUserCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()

	// Execute the command
	cmd := NewRegisterUserCommand(&app.Config{
		KeySecp: *key1,
		KeyP521: *key2,
	}, true).Command()
	cmd.Run(nil, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "Public key registered successfully")
}