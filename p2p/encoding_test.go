package p2p

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFramedProtocol_RoundTrip(t *testing.T) {
	decoder := NewFramedDecoder()

	// 1. Message frame round trip
	msgPayload := []byte("NazeerDFS control message payload")
	streamID := uint64(123456789)

	frame, err := EncodeMessageFrame(streamID, msgPayload)
	require.NoError(t, err)

	rpc := RPC{}
	err = decoder.Decode(bytes.NewReader(frame), &rpc)
	require.NoError(t, err)

	assert.False(t, rpc.Stream)
	assert.Equal(t, streamID, rpc.StreamID)
	assert.Equal(t, msgPayload, rpc.Payload)

	// 2. Stream chunk frame round trip
	streamChunk := []byte("Encrypted chunk binary stream data")
	chunkFrame, err := EncodeStreamFrame(streamID, streamChunk)
	require.NoError(t, err)

	rpc2 := RPC{}
	err = decoder.Decode(bytes.NewReader(chunkFrame), &rpc2)
	require.NoError(t, err)

	assert.True(t, rpc2.Stream)
	assert.Equal(t, streamID, rpc2.StreamID)
	assert.Equal(t, streamChunk, rpc2.Payload)
}

func TestFramedProtocol_InvalidMagic(t *testing.T) {
	decoder := NewFramedDecoder()

	badMagicFrame := []byte{0x00, 0x00, FrameTypeMessage, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 4, 1, 2, 3, 4}
	rpc := RPC{}
	err := decoder.Decode(bytes.NewReader(badMagicFrame), &rpc)
	assert.ErrorIs(t, err, ErrInvalidMagic)
}

func TestFramedProtocol_BoundedMessageSize(t *testing.T) {
	decoder := NewFramedDecoder()

	// Frame header specifying payload larger than MaxFramePayloadSize (4MB)
	hdr := make([]byte, FrameHeaderSize)
	hdr[0] = FrameMagic0
	hdr[1] = FrameMagic1
	hdr[2] = FrameTypeMessage
	// length = 5MB
	hdr[11] = 0x00
	hdr[12] = 0x50
	hdr[13] = 0x00
	hdr[14] = 0x00

	rpc := RPC{}
	err := decoder.Decode(bytes.NewReader(hdr), &rpc)
	assert.ErrorIs(t, err, ErrFrameTooLarge)
}

func TestFramedProtocol_TruncatedFrame(t *testing.T) {
	decoder := NewFramedDecoder()

	frame, err := EncodeMessageFrame(1, []byte("Test payload data"))
	require.NoError(t, err)

	// Truncate frame
	truncated := frame[:len(frame)-5]
	rpc := RPC{}
	err = decoder.Decode(bytes.NewReader(truncated), &rpc)
	assert.Error(t, err)
}

// Fuzz test for framed decoder to ensure no panics or unbounded memory allocation
func FuzzFramedDecoder(f *testing.F) {
	// Seed valid frames
	validFrame, _ := EncodeMessageFrame(42, []byte("Hello fuzz"))
	f.Add(validFrame)

	emptyFrame, _ := EncodeStreamFrame(1, []byte{})
	f.Add(emptyFrame)

	// Random corrupt seed
	randomBuf := make([]byte, 100)
	_, _ = rand.Read(randomBuf)
	f.Add(randomBuf)

	decoder := NewFramedDecoder()

	f.Fuzz(func(t *testing.T, data []byte) {
		rpc := RPC{}
		// Decoder must handle any arbitrary random bytes without panicking
		_ = decoder.Decode(bytes.NewReader(data), &rpc)
	})
}
