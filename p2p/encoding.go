package p2p

import (
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
)

const (
	// FrameMagic0 and FrameMagic1 are the 2-byte header magic identifiers ("ND" for NazeerDFS).
	FrameMagic0 = 0x4E
	FrameMagic1 = 0x44

	// Frame types
	FrameTypeMessage = 0x01
	FrameTypeStream  = 0x02

	// FrameHeaderSize is the 15-byte fixed header: Magic (2B) | Type (1B) | StreamID (8B) | PayloadLen (4B)
	FrameHeaderSize = 15

	// MaxFramePayloadSize limits single frame size to 4MB to prevent memory exhaustion attacks.
	MaxFramePayloadSize uint32 = 4 * 1024 * 1024
)

var (
	ErrInvalidMagic   = errors.New("invalid frame magic bytes")
	ErrFrameTooLarge  = errors.New("frame payload length exceeds maximum allowed limit")
	ErrCorruptPayload = errors.New("failed to read complete frame payload")
)

// Decoder is an interface that can be implemented to decode
// a message from a reader into an RPC message.
type Decoder interface {
	Decode(io.Reader, *RPC) error
}

// GOBDecoder is a decoder that uses the gob package to decode
// messages from a reader into an RPC message.
type GOBDecoder struct{}

func (d *GOBDecoder) Decode(r io.Reader, msg *RPC) error {
	return gob.NewDecoder(r).Decode(msg)
}

// FramedDecoder decodes length-prefixed binary frames safely with bounded message limits.
type FramedDecoder struct {
	MaxPayloadSize uint32
}

func NewFramedDecoder() *FramedDecoder {
	return &FramedDecoder{MaxPayloadSize: MaxFramePayloadSize}
}

func (d *FramedDecoder) Decode(r io.Reader, msg *RPC) error {
	var hdr [FrameHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}

	if hdr[0] != FrameMagic0 || hdr[1] != FrameMagic1 {
		return ErrInvalidMagic
	}

	frameType := hdr[2]
	streamID := binary.BigEndian.Uint64(hdr[3:11])
	length := binary.BigEndian.Uint32(hdr[11:15])

	maxSize := d.MaxPayloadSize
	if maxSize == 0 {
		maxSize = MaxFramePayloadSize
	}
	if length > maxSize {
		return ErrFrameTooLarge
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return fmt.Errorf("%w: %v", ErrCorruptPayload, err)
		}
	}

	msg.Stream = (frameType == FrameTypeStream)
	msg.StreamID = streamID
	msg.Payload = payload
	return nil
}

// DefaultDecoder supports framed protocol while providing graceful fallback.
type DefaultDecoder struct {
	framed *FramedDecoder
}

func (d DefaultDecoder) Decode(r io.Reader, msg *RPC) error {
	if d.framed == nil {
		d.framed = NewFramedDecoder()
	}
	return d.framed.Decode(r, msg)
}

// EncodeFrame creates a length-prefixed frame.
func EncodeFrame(frameType byte, streamID uint64, payload []byte) ([]byte, error) {
	if len(payload) > int(MaxFramePayloadSize) {
		return nil, ErrFrameTooLarge
	}

	frame := make([]byte, FrameHeaderSize+len(payload))
	frame[0] = FrameMagic0
	frame[1] = FrameMagic1
	frame[2] = frameType
	binary.BigEndian.PutUint64(frame[3:11], streamID)
	binary.BigEndian.PutUint32(frame[11:15], uint32(len(payload)))
	copy(frame[15:], payload)
	return frame, nil
}

// EncodeMessageFrame encodes a control message into a framed byte slice.
func EncodeMessageFrame(streamID uint64, payload []byte) ([]byte, error) {
	return EncodeFrame(FrameTypeMessage, streamID, payload)
}

// EncodeStreamFrame encodes a stream chunk into a framed byte slice.
func EncodeStreamFrame(streamID uint64, chunk []byte) ([]byte, error) {
	return EncodeFrame(FrameTypeStream, streamID, chunk)
}
