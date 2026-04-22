package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HorizenOfficial/vela/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveApplicationID_NewEntry(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, os.WriteFile(conf, []byte("keyP521=\nkeySecp256k1=\n"), 0o644))

	err := SaveApplicationID(conf, common.NewApplicationId(42))
	require.NoError(t, err)

	data, _ := os.ReadFile(conf)
	content := string(data)
	assert.Contains(t, content, "ApplicationID=42")
	assert.Contains(t, content, "keyP521=")
}

func TestSaveApplicationID_ReplacesExisting(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, os.WriteFile(conf, []byte("keyP521=\nApplicationID=7\nkeySecp256k1=\n"), 0o644))

	err := SaveApplicationID(conf, common.NewApplicationId(42))
	require.NoError(t, err)

	data, _ := os.ReadFile(conf)
	content := string(data)
	assert.Contains(t, content, "ApplicationID=42")
	assert.Contains(t, content, "# ApplicationID=7")
	assert.Contains(t, content, "# Previous ApplicationID (replaced by deployapp on")
	// Old value should not appear as an active line
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "ApplicationID=7" {
			t.Error("old ApplicationID=7 should be commented out, not active")
		}
	}
}

func TestSaveApplicationID_SameValueNoComment(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, os.WriteFile(conf, []byte("ApplicationID=42\n"), 0o644))

	err := SaveApplicationID(conf, common.NewApplicationId(42))
	require.NoError(t, err)

	data, _ := os.ReadFile(conf)
	content := string(data)
	assert.Contains(t, content, "ApplicationID=42")
	assert.NotContains(t, content, "# Previous ApplicationID")
}

func TestSaveApplicationID_SpacesAroundEquals(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, os.WriteFile(conf, []byte("keyP521=\nApplicationID = 7\nkeySecp256k1=\n"), 0o644))

	err := SaveApplicationID(conf, common.NewApplicationId(42))
	require.NoError(t, err)

	data, _ := os.ReadFile(conf)
	content := string(data)
	assert.Contains(t, content, "ApplicationID=42")
	assert.Contains(t, content, "# ApplicationID = 7")
	// Old value should not appear as an active line
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "ApplicationID = 7" {
			t.Error("old 'ApplicationID = 7' should be commented out, not active")
		}
	}
}

func TestSaveApplicationID_EmptyValue(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "wallet.conf")
	require.NoError(t, os.WriteFile(conf, []byte("ApplicationID=\n"), 0o644))

	err := SaveApplicationID(conf, common.NewApplicationId(99))
	require.NoError(t, err)

	data, _ := os.ReadFile(conf)
	content := string(data)
	assert.Contains(t, content, "ApplicationID=99")
	assert.NotContains(t, content, "# Previous ApplicationID")
}
