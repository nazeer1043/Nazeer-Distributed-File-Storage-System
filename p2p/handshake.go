package p2p

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrHandshakeTimeout = errors.New("p2p handshake timed out")
	ErrUnauthorizedPeer = errors.New("p2p authentication failed: unauthorized cluster secret")
)

// HandshakeFunc is a function that performs a handshake with a peer.
type HandshakeFunc func(Peer) error

// NOPHandshakeFunc is a no-op handshake function.
func NOPHandshakeFunc(Peer) error { return nil }

// HMACClusterHandshake creates a mutual challenge-response authentication handshake
// using the shared cluster secret. Unauthenticated peers are rejected immediately.
func HMACClusterHandshake(clusterSecret string) HandshakeFunc {
	return func(p Peer) error {
		if clusterSecret == "" {
			return errors.New("cluster secret cannot be empty for mutual authentication")
		}

		// 5-second deadline for the handshake
		_ = p.SetDeadline(time.Now().Add(5 * time.Second))
		defer func() {
			_ = p.SetDeadline(time.Time{})
		}()

		tcpPeer, ok := p.(*TCPPeer)
		isOutbound := false
		if ok {
			isOutbound = tcpPeer.outbound
		}

		mac := func(data []byte) []byte {
			h := hmac.New(sha256.New, []byte(clusterSecret))
			h.Write(data)
			return h.Sum(nil)
		}

		if isOutbound {
			// Initiator:
			// 1. Read 32-byte challenge from Receiver
			challengeRecv := make([]byte, 32)
			if _, err := io.ReadFull(p, challengeRecv); err != nil {
				return fmt.Errorf("initiator failed to read challenge from receiver: %w", err)
			}

			// 2. Generate 32-byte challenge for Receiver
			challengeInit := make([]byte, 32)
			if _, err := io.ReadFull(rand.Reader, challengeInit); err != nil {
				return err
			}

			// 3. Compute HMAC response to Receiver's challenge
			respRecv := mac(challengeRecv)

			// 4. Send challengeInit (32B) + respRecv (32B) = 64 bytes
			payload := append(challengeInit, respRecv...)
			if _, err := p.Write(payload); err != nil {
				return fmt.Errorf("initiator failed to send handshake response: %w", err)
			}

			// 5. Read 32-byte response from Receiver
			respInit := make([]byte, 32)
			if _, err := io.ReadFull(p, respInit); err != nil {
				return fmt.Errorf("initiator failed to read response from receiver: %w", err)
			}

			// 6. Verify response
			expectedRespInit := mac(challengeInit)
			if subtle.ConstantTimeCompare(respInit, expectedRespInit) != 1 {
				return ErrUnauthorizedPeer
			}

		} else {
			// Receiver:
			// 1. Generate 32-byte challenge for Initiator
			challengeRecv := make([]byte, 32)
			if _, err := io.ReadFull(rand.Reader, challengeRecv); err != nil {
				return err
			}

			// 2. Send challenge to Initiator
			if _, err := p.Write(challengeRecv); err != nil {
				return fmt.Errorf("receiver failed to send challenge: %w", err)
			}

			// 3. Read 64 bytes (challengeInit + respRecv) from Initiator
			payload := make([]byte, 64)
			if _, err := io.ReadFull(p, payload); err != nil {
				return fmt.Errorf("receiver failed to read challenge response: %w", err)
			}
			challengeInit := payload[:32]
			respRecv := payload[32:]

			// 4. Verify Initiator's response
			expectedRespRecv := mac(challengeRecv)
			if subtle.ConstantTimeCompare(respRecv, expectedRespRecv) != 1 {
				return ErrUnauthorizedPeer
			}

			// 5. Send response to Initiator's challenge
			respInit := mac(challengeInit)
			if _, err := p.Write(respInit); err != nil {
				return fmt.Errorf("receiver failed to send response: %w", err)
			}
		}

		if ok {
			tcpPeer.authenticated = true
		}
		return nil
	}
}
