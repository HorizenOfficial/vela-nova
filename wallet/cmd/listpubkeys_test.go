package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/HorizenOfficial/vela-nova/wallet/app"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	"github.com/stretchr/testify/assert"
)

func TestListPubKeysCmd(t *testing.T) {
	// Redirect stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var key1, _ = crypto.GeneratePrivateKeySecp256k1()
	var key2, _ = crypto.GeneratePrivateKeyP521()
	expectedPubKey1 := crypto.ExportPublicKeySecp256k1ToHex(key1.PublicKey())
	expectedPubKey2 := crypto.ExportPublicKeyP521ToHex(key2.PublicKey())

	// Execute the command
	cmd := NewListPubKeysCommand(&app.Config{
		KeySecp: key1,
		KeyP521: key2,
	}).Command()
	cmd.Run(cmd, nil)

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, expectedPubKey1)
	assert.Contains(t, output, expectedPubKey2)
}
