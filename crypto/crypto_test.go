package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthenticatedChunked_RoundTrip(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)

	payload := []byte("The quick brown fox jumps over the lazy dog. NazeerDFS enterprise secure file storage.")
	encDst := new(bytes.Buffer)

	nWritten, err := EncryptStream(key, "k1", bytes.NewReader(payload), encDst)
	require.NoError(t, err)
	assert.Greater(t, nWritten, int64(len(payload)))

	decDst := new(bytes.Buffer)
	nDecrypted, err := DecryptStream(key, encDst, decDst)
	require.NoError(t, err)
	assert.Equal(t, int64(len(payload)), nDecrypted)
	assert.Equal(t, payload, decDst.Bytes())
}

func TestAuthenticatedChunked_TamperDetection(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)

	payload := []byte("Sensitive data that must not be modified in transit or at rest.")
	encDst := new(bytes.Buffer)
	_, err = EncryptStream(key, "k1", bytes.NewReader(payload), encDst)
	require.NoError(t, err)

	ciphertext := encDst.Bytes()
	// Tamper with a byte in the chunk ciphertext body
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-5] ^= 0xAA

	decDst := new(bytes.Buffer)
	_, err = DecryptStream(key, bytes.NewReader(tampered), decDst)
	assert.Error(t, err, "tampered ciphertext must fail authentication")
	assert.ErrorIs(t, err, ErrAuthFailed)
}

func TestAuthenticatedChunked_Truncation(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)

	// Create data larger than one chunk (DefaultChunkSize is 64KB)
	largeData := make([]byte, 128*1024)
	_, err = rand.Read(largeData)
	require.NoError(t, err)

	encDst := new(bytes.Buffer)
	_, err = EncryptStream(key, "k1", bytes.NewReader(largeData), encDst)
	require.NoError(t, err)

	// Truncate the stream by slicing off the last 100 bytes (omitting final tag and isLast frame)
	truncated := encDst.Bytes()[:encDst.Len()-100]

	decDst := new(bytes.Buffer)
	_, err = DecryptStream(key, bytes.NewReader(truncated), decDst)
	assert.Error(t, err, "truncated stream must fail")
	assert.ErrorIs(t, err, ErrTruncatedStream)
}

func TestAuthenticatedChunked_WrongKey(t *testing.T) {
	key1, _ := NewEncryptionKey()
	key2, _ := NewEncryptionKey()

	payload := []byte("Top secret payload")
	encDst := new(bytes.Buffer)
	_, err := EncryptStream(key1, "k1", bytes.NewReader(payload), encDst)
	require.NoError(t, err)

	decDst := new(bytes.Buffer)
	_, err = DecryptStream(key2, encDst, decDst)
	assert.Error(t, err, "decrypting with wrong key must fail authentication")
	assert.ErrorIs(t, err, ErrAuthFailed)
}

func TestAuthenticatedChunked_EmptyFile(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)

	encDst := new(bytes.Buffer)
	nWritten, err := EncryptStream(key, "k1", bytes.NewReader([]byte{}), encDst)
	require.NoError(t, err)
	assert.Greater(t, nWritten, int64(0)) // Header + empty final chunk frame written

	decDst := new(bytes.Buffer)
	nDecrypted, err := DecryptStream(key, encDst, decDst)
	require.NoError(t, err)
	assert.Equal(t, int64(0), nDecrypted)
	assert.Empty(t, decDst.Bytes())
}

func TestAuthenticatedChunked_VeryLargeFile(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)

	// Test 300 KB payload spanning 5 chunks (64KB each)
	totalBytes := 300 * 1024
	largePayload := make([]byte, totalBytes)
	for i := range largePayload {
		largePayload[i] = byte(i % 251)
	}

	encDst := new(bytes.Buffer)
	nEncrypted, err := EncryptStream(key, "k1", bytes.NewReader(largePayload), encDst)
	require.NoError(t, err)
	assert.Greater(t, nEncrypted, int64(totalBytes))

	decDst := new(bytes.Buffer)
	nDecrypted, err := DecryptStream(key, encDst, decDst)
	require.NoError(t, err)
	assert.Equal(t, int64(totalBytes), nDecrypted)
	assert.Equal(t, largePayload, decDst.Bytes())
}

func TestCopyStream_WriteFailureReturnsCorrectError(t *testing.T) {
	key, err := NewEncryptionKey()
	require.NoError(t, err)

	payload := []byte("Hello stream test")
	src := bytes.NewReader(payload)
	dst := new(bytes.Buffer)

	// CopyEncrypt and CopyDecrypt using authenticated chunked streams
	nEnc, err := CopyEncrypt(key, src, dst)
	require.NoError(t, err)
	assert.Greater(t, nEnc, len(payload))

	out := new(bytes.Buffer)
	nDec, err := CopyDecrypt(key, dst, out)
	require.NoError(t, err)
	assert.Equal(t, len(payload), nDec)
	assert.Equal(t, payload, out.Bytes())
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
