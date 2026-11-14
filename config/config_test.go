package config

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_LoadAndValidate_Development(t *testing.T) {
	// Clear any existing env
	os.Unsetenv("APP_ENV")
	os.Unsetenv("ENCRYPTION_KEY")
	os.Unsetenv("CLUSTER_SECRET")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, "8080", cfg.HTTPPort)
	assert.Equal(t, 32, len(cfg.EncryptionKey))

	err = cfg.Validate()
	assert.NoError(t, err)
}

func TestConfig_LoadAndValidate_Production_RequiresStrictSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PORT", "8080")
	t.Setenv("NODE_LISTEN_ADDRS", ":3000,:4000")
	t.Setenv("STORAGE_ROOTS", "data1,data2")

	// Missing production encryption key
	cfg, err := LoadConfig()
	require.NoError(t, err)
	err = cfg.Validate()
	assert.Error(t, err, "should fail without production key and cluster secret")

	// Set valid 32-byte base64 encryption key and cluster secret
	validKey := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	t.Setenv("ENCRYPTION_KEY", validKey)
	t.Setenv("CLUSTER_SECRET", "super-secret-cluster-token-123456")
	t.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "SuperSecurePassword123!")

	cfg, err = LoadConfig()
	require.NoError(t, err)
	err = cfg.Validate()
	assert.NoError(t, err)
	assert.Equal(t, []byte("01234567890123456789012345678901"), cfg.EncryptionKey)
}

func TestConfig_Validate_InvalidBase64Key(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "not-base64-content!@#$")
	_, err := LoadConfig()
	assert.Error(t, err)
}
