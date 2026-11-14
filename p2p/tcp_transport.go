package p2p

import (
	"errors"
	"log"
	"net"
	"time"
)

// ErrNilListener is an error that is returned when the listener is nil.
var ErrNilListener = errors.New("listener is nil")

// TCPTransport is a transport implementation that uses TCP as the underlying network protocol.
type TCPTransport struct {
	ListenAddr       string
	ShakeHands       HandshakeFunc
	Decoder          Decoder
	OnPeer           func(Peer) error
	OnPeerDisconnect func(Peer)

	listener net.Listener
	rpcCh    chan RPC
}

// TCPTransportOption is a functional option type for configuring a TCPTransport.
type TCPTransportOption func(*TCPTransport)

// WithListenAddr is a functional option for setting the listen address of the TCPTransport.
func WithListenAddr(addr string) TCPTransportOption {
	return func(t *TCPTransport) {
		t.ListenAddr = addr
	}
}

// WithHandshakeFunc is a functional option for setting the handshake function of the TCPTransport.
func WithHandshakeFunc(h HandshakeFunc) TCPTransportOption {
	return func(t *TCPTransport) {
		t.ShakeHands = h
	}
}

// WithDecoder is a functional option for setting the decoder of the TCPTransport.
func WithDecoder(d Decoder) TCPTransportOption {
	return func(t *TCPTransport) {
		t.Decoder = d
	}
}

// WithOnPeer is a functional option for setting the on peer function of the TCPTransport.
func WithOnPeer(f func(Peer) error) TCPTransportOption {
	return func(t *TCPTransport) {
		t.OnPeer = f
	}
}

// WithOnPeerDisconnect is a functional option for setting the peer disconnect cleanup function.
func WithOnPeerDisconnect(f func(Peer)) TCPTransportOption {
	return func(t *TCPTransport) {
		t.OnPeerDisconnect = f
	}
}

// NewTCPTransport creates a new TCPTransport with the given options.
func NewTCPTransport(opts ...TCPTransportOption) *TCPTransport {
	t := &TCPTransport{
		rpcCh: make(chan RPC, 1024),
	}
	for _, opt := range opts {
		opt(t)
	}
	if t.Decoder == nil {
		t.Decoder = NewFramedDecoder()
	}
	if t.ShakeHands == nil {
		t.ShakeHands = NOPHandshakeFunc
	}
	return t
}

// Consume implements the Transport interface.
func (t *TCPTransport) Consume() <-chan RPC {
	return t.rpcCh
}

// Close implements the Transport interface.
func (t *TCPTransport) Close() error {
	if t.listener != nil {
		return t.listener.Close()
	}
	return ErrNilListener
}

// Dial implements the Transport interface.
func (t *TCPTransport) Dial(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return err
	}

	go t.handleConn(conn, true)
	return nil
}

// Addr implements the Transport interface.
func (t *TCPTransport) Addr() string {
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.ListenAddr
}

// ListenAndAccept implements the Transport interface.
func (t *TCPTransport) ListenAndAccept() error {
	var err error

	t.listener, err = net.Listen("tcp", t.ListenAddr)
	if err != nil {
		log.Printf("tcp listen error: %s\n", err.Error())
		return err
	}
	t.ListenAddr = t.listener.Addr().String()

	go t.startAcceptLoop()
	log.Printf("TCP transport listening on port [%s]\n", t.ListenAddr)
	return nil
}

func (t *TCPTransport) startAcceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}

		if err != nil {
			log.Printf("tcp accept error: %s\n", err.Error())
			continue
		}

		go t.handleConn(conn, false)
	}
}

func (t *TCPTransport) handleConn(conn net.Conn, outbound bool) {
	var err error

	peer := NewTCPPeer(
		WithTCPPeerConn(conn),
		WithTCPPeerOutbound(outbound),
	)

	defer func() {
		if t.OnPeerDisconnect != nil {
			t.OnPeerDisconnect(peer)
		}
		if err != nil {
			log.Printf("[%s] dropping peer connection: %s\n", conn.RemoteAddr(), err)
		}
		_ = conn.Close()
	}()

	if err = t.ShakeHands(peer); err != nil {
		return
	}

	if t.OnPeer != nil {
		if err = t.OnPeer(peer); err != nil {
			return
		}
	}

	// Read loop with bounded read deadlines
	for {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		rpc := RPC{}
		err = t.Decoder.Decode(conn, &rpc)
		if err != nil {
			return
		}

		rpc.From = conn.RemoteAddr()
		t.rpcCh <- rpc
	}
}
