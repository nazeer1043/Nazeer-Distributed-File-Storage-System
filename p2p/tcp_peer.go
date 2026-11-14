package p2p

import (
	"net"
	"sync"
	"time"
)

// TCPPeer represents a peer in a TCP network.
type TCPPeer struct {
	net.Conn
	outbound      bool
	authenticated bool
	wg            *sync.WaitGroup
}

// TCPPeerOption is a functional option for configuring a TCPPeer.
type TCPPeerOption func(*TCPPeer)

// WithTCPPeerConn is a functional option for setting the connection of the TCPPeer.
func WithTCPPeerConn(conn net.Conn) TCPPeerOption {
	return func(p *TCPPeer) {
		p.Conn = conn
	}
}

// WithTCPPeerOutbound is a functional option for setting the outbound flag of the TCPPeer.
func WithTCPPeerOutbound(outbound bool) TCPPeerOption {
	return func(p *TCPPeer) {
		p.outbound = outbound
	}
}

// NewTCPPeer creates a new TCPPeer with the given options.
func NewTCPPeer(opts ...TCPPeerOption) *TCPPeer {
	p := &TCPPeer{
		wg: &sync.WaitGroup{},
	}

	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Send sends data to the peer with a write deadline.
func (p *TCPPeer) Send(data []byte) error {
	_ = p.Conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	defer func() {
		_ = p.Conn.SetWriteDeadline(time.Time{})
	}()
	_, err := p.Conn.Write(data)
	return err
}

// CloseStream closes the stream of the peer.
func (p *TCPPeer) CloseStream() {
	p.wg.Done()
}

// IsAuthenticated reports whether the peer successfully passed mutual authentication.
func (p *TCPPeer) IsAuthenticated() bool {
	return p.authenticated
}

// SetAuthenticated explicitly updates peer authentication state.
func (p *TCPPeer) SetAuthenticated(auth bool) {
	p.authenticated = auth
}
