package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyEncrypt(t *testing.T) {
	payLoad := []byte("Foo not Bar")
	src := bytes.NewReader(payLoad)
	dst := new(bytes.Buffer)
	key, err := NewEncryptionKey()
	require.NoError(t, err)
	assert.Len(t, key, 32)

	_, err = CopyEncrypt(key, src, dst)
	require.NoError(t, err)

	out := new(bytes.Buffer)
	nw, err := CopyDecrypt(key, dst, out)
	require.NoError(t, err)

	assert.Equal(t, 16+len(payLoad), nw)
	assert.Equal(t, payLoad, out.Bytes())
}

func TestAES256GCM_EncryptDecrypt(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)

	message := []byte("Highly confidential distributed filesystem payload")
	ciphertext, err := EncryptGCM(key, message)
	require.NoError(t, err)
	assert.NotEqual(t, message, ciphertext)

	plaintext, err := DecryptGCM(key, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, message, plaintext)

	// Tampered ciphertext should fail authentication
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xFF
	_, err = DecryptGCM(key, tampered)
	assert.Error(t, err, "tampered ciphertext must fail authentication")

	// Short ciphertext should return error
	_, err = DecryptGCM(key, []byte("short"))
	assert.Error(t, err)
}

func TestCryptoUtilities(t *testing.T) {
	id1 := GenerateID()
	id2 := GenerateID()
	assert.NotEmpty(t, id1)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 64) // 32 bytes hex encoded

	hash1 := HashKey("file_key_1")
	hash2 := HashKey("file_key_1")
	hash3 := HashKey("file_key_2")
	assert.Equal(t, hash1, hash2)
	assert.NotEqual(t, hash1, hash3)
	assert.Len(t, hash1, 64) // SHA-256 hex string
}

