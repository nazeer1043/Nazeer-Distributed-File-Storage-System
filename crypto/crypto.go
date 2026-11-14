package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const (
	// HeaderMagic identifies a NazeerDFS encrypted stream.
	HeaderMagic = "NDFS"
	// CurrentVersion represents the authenticated chunked encryption version.
	CurrentVersion byte = 1
	// DefaultChunkSize is 64KB for chunked authenticated streaming.
	DefaultChunkSize uint32 = 64 * 1024
)

var (
	ErrInvalidHeader      = errors.New("crypto: invalid NazeerDFS ciphertext header")
	ErrUnsupportedVersion = errors.New("crypto: unsupported encryption format version")
	ErrAuthFailed         = errors.New("crypto: authenticated chunk verification failed")
	ErrChunkOutOfOrder    = errors.New("crypto: chunk received out of sequence")
	ErrTruncatedStream    = errors.New("crypto: ciphertext stream truncated before final chunk")
	ErrKeySize            = errors.New("crypto: encryption key must be exactly 32 bytes for AES-256")
)

// GenerateID generates a new random hexadecimal unique ID.
func GenerateID() string {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// HashKey generates a secure SHA-256 hash string for the given key.
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// NewEncryptionKey generates a new cryptographically secure 256-bit AES encryption key.
func NewEncryptionKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// deriveNonce generates a unique 12-byte GCM nonce from a base 12-byte salt and chunk index.
func deriveNonce(baseNonce []byte, chunkIndex uint64) []byte {
	nonce := make([]byte, 12)
	copy(nonce, baseNonce)
	baseCounter := binary.BigEndian.Uint64(baseNonce[4:12])
	binary.BigEndian.PutUint64(nonce[4:12], baseCounter^chunkIndex)
	return nonce
}

// makeAAD builds the Additional Authenticated Data bound to every chunk.
func makeAAD(keyID string, chunkIndex uint64, isLast byte) []byte {
	aad := make([]byte, 4+len(keyID)+8+1)
	copy(aad[0:4], []byte(HeaderMagic))
	copy(aad[4:4+len(keyID)], []byte(keyID))
	offset := 4 + len(keyID)
	binary.BigEndian.PutUint64(aad[offset:offset+8], chunkIndex)
	aad[offset+8] = isLast
	return aad
}

// EncryptStream reads from src, encrypts using authenticated chunked AES-256-GCM,
// prepends a versioned header with keyID, and writes the stream to dst.
func EncryptStream(key []byte, keyID string, src io.Reader, dst io.Writer) (int64, error) {
	if len(key) != 32 {
		return 0, ErrKeySize
	}
	if keyID == "" {
		keyID = "k1"
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, err
	}

	// 1. Generate base nonce (12 bytes)
	baseNonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, baseNonce); err != nil {
		return 0, fmt.Errorf("failed to generate random base nonce: %w", err)
	}

	// 2. Write Header:
	// Magic (4B) | Version (1B) | Flags (1B) | KeyIDLen (2B) | KeyID (N B) | ChunkSize (4B) | BaseNonce (12B)
	keyIDBytes := []byte(keyID)
	headerLen := 4 + 1 + 1 + 2 + len(keyIDBytes) + 4 + 12
	hdr := make([]byte, headerLen)
	copy(hdr[0:4], []byte(HeaderMagic))
	hdr[4] = CurrentVersion
	hdr[5] = 0 // flags
	binary.BigEndian.PutUint16(hdr[6:8], uint16(len(keyIDBytes)))
	copy(hdr[8:8+len(keyIDBytes)], keyIDBytes)
	offset := 8 + len(keyIDBytes)
	binary.BigEndian.PutUint32(hdr[offset:offset+4], DefaultChunkSize)
	copy(hdr[offset+4:offset+16], baseNonce)

	var totalWritten int64
	nw, err := dst.Write(hdr)
	if err != nil {
		return 0, err
	}
	totalWritten += int64(nw)

	// 3. Process plaintext in chunks
	chunkBuf := make([]byte, DefaultChunkSize)
	var chunkIndex uint64
	for {
		nr, readErr := io.ReadFull(src, chunkBuf)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return totalWritten, readErr
		}

		isLast := byte(0)
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			isLast = 1
		}

		plaintextChunk := chunkBuf[:nr]
		nonce := deriveNonce(baseNonce, chunkIndex)
		aad := makeAAD(keyID, chunkIndex, isLast)

		ciphertext := gcm.Seal(nil, nonce, plaintextChunk, aad)

		// Frame: ChunkIndex (8B) | IsLast (1B) | CiphertextLen (4B) | Ciphertext
		frameHdr := make([]byte, 8+1+4)
		binary.BigEndian.PutUint64(frameHdr[0:8], chunkIndex)
		frameHdr[8] = isLast
		binary.BigEndian.PutUint32(frameHdr[9:13], uint32(len(ciphertext)))

		nw, err := dst.Write(frameHdr)
		if err != nil {
			return totalWritten, err
		}
		totalWritten += int64(nw)

		nw, err = dst.Write(ciphertext)
		if err != nil {
			return totalWritten, err
		}
		totalWritten += int64(nw)

		chunkIndex++
		if isLast == 1 {
			break
		}
	}

	return totalWritten, nil
}

// DecryptStream reads authenticated chunked ciphertext from src, verifies each chunk's
// authenticity, tag, and sequencing, and writes decrypted plaintext to dst.
func DecryptStream(key []byte, src io.Reader, dst io.Writer) (int64, error) {
	if len(key) != 32 {
		return 0, ErrKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, err
	}

	// 1. Read static header prefix (Magic 4B + Version 1B + Flags 1B + KeyIDLen 2B = 8B)
	preHdr := make([]byte, 8)
	if _, err := io.ReadFull(src, preHdr); err != nil {
		return 0, fmt.Errorf("%w: failed to read magic: %v", ErrInvalidHeader, err)
	}

	if string(preHdr[0:4]) != HeaderMagic {
		return 0, fmt.Errorf("%w: unexpected magic bytes %q", ErrInvalidHeader, preHdr[0:4])
	}
	version := preHdr[4]
	if version != CurrentVersion {
		return 0, fmt.Errorf("%w: found version %d", ErrUnsupportedVersion, version)
	}
	keyIDLen := binary.BigEndian.Uint16(preHdr[6:8])

	// Read KeyID (keyIDLen B) + ChunkSize (4B) + BaseNonce (12B)
	remHdrLen := int(keyIDLen) + 4 + 12
	remHdr := make([]byte, remHdrLen)
	if _, err := io.ReadFull(src, remHdr); err != nil {
		return 0, fmt.Errorf("%w: failed to read header body: %v", ErrInvalidHeader, err)
	}

	keyID := string(remHdr[0:keyIDLen])
	// chunkSize := binary.BigEndian.Uint32(remHdr[keyIDLen : keyIDLen+4])
	baseNonce := remHdr[keyIDLen+4 : keyIDLen+16]

	// 2. Read and verify chunks sequentially
	frameHdr := make([]byte, 8+1+4)
	var expectedChunkIndex uint64
	var totalPlaintextWritten int64
	var foundFinalChunk bool

	for {
		_, err := io.ReadFull(src, frameHdr)
		if errors.Is(err, io.EOF) {
			if !foundFinalChunk {
				return totalPlaintextWritten, ErrTruncatedStream
			}
			break
		}
		if err != nil {
			return totalPlaintextWritten, fmt.Errorf("%w: chunk frame read error: %v", ErrTruncatedStream, err)
		}

		chunkIdx := binary.BigEndian.Uint64(frameHdr[0:8])
		isLast := frameHdr[8]
		ciphertextLen := binary.BigEndian.Uint32(frameHdr[9:13])

		if chunkIdx != expectedChunkIndex {
			return totalPlaintextWritten, fmt.Errorf("%w: expected %d, got %d", ErrChunkOutOfOrder, expectedChunkIndex, chunkIdx)
		}

		ciphertext := make([]byte, ciphertextLen)
		if _, err := io.ReadFull(src, ciphertext); err != nil {
			return totalPlaintextWritten, fmt.Errorf("%w: failed to read full chunk ciphertext: %v", ErrTruncatedStream, err)
		}

		nonce := deriveNonce(baseNonce, chunkIdx)
		aad := makeAAD(keyID, chunkIdx, isLast)

		plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
		if err != nil {
			return totalPlaintextWritten, fmt.Errorf("%w: chunk %d verification failed: %v", ErrAuthFailed, chunkIdx, err)
		}

		if len(plaintext) > 0 {
			nw, err := dst.Write(plaintext)
			if err != nil {
				return totalPlaintextWritten, err
			}
			totalPlaintextWritten += int64(nw)
		}

		expectedChunkIndex++
		if isLast == 1 {
			foundFinalChunk = true
			break
		}
	}

	if !foundFinalChunk {
		return totalPlaintextWritten, ErrTruncatedStream
	}

	return totalPlaintextWritten, nil
}

// CopyEncrypt reads from src, encrypts the stream with authenticated chunked encryption, and writes to dst.
func CopyEncrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	n, err := EncryptStream(key, "k1", src, dst)
	return int(n), err
}

// CopyDecrypt reads from src, decrypts the authenticated chunked stream, and writes to dst.
func CopyDecrypt(key []byte, src io.Reader, dst io.Writer) (int, error) {
	n, err := DecryptStream(key, src, dst)
	return int(n), err
}

// EncryptGCM encrypts plaintext using authenticated AES-256-GCM.
// Returns a combined slice of 12-byte nonce followed by ciphertext and authentication tag.
func EncryptGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptGCM decrypts ciphertext using authenticated AES-256-GCM.
// Expects nonce prepended to the ciphertext and verifies tag authentication.
func DecryptGCM(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext length %d is shorter than GCM nonce size %d", len(ciphertext), nonceSize)
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, actualCiphertext, nil)
}

// copyStream is the legacy CTR streaming helper, kept for compatibility with bugfixes applied:
// returns the actual write error on destination failure, and tracks exact byte count.
func copyStream(stream cipher.Stream, blockSize int, src io.Reader, dst io.Writer) (int, error) {
	var (
		buf = make([]byte, 1024*32)
		nw  = 0
	)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			stream.XORKeyStream(buf, buf[:n])
			nn, err2 := dst.Write(buf[:n])
			if err2 != nil {
				return nw, err2
			}
			nw += nn
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nw, err
		}
	}
	return nw, nil
}
