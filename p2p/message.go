package p2p

import "net"

const (
	// IncomingMessage represents an incoming message control frame.
	IncomingMessage = 0x1
	// IncomingStream represents an incoming binary stream frame.
	IncomingStream = 0x2
)

// RPC holds any arbitrary data that is being sent over
// each transport between two nodes in the network.
type RPC struct {
	From     net.Addr
	StreamID uint64
	Payload  []byte
	Stream   bool
}
