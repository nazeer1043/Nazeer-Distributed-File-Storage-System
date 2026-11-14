package p2p

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMutualHMAC_Authentication_Success(t *testing.T) {
	clusterSecret := "super-secure-production-cluster-secret-2026"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	receiverHandshake := HMACClusterHandshake(clusterSecret)
	initiatorHandshake := HMACClusterHandshake(clusterSecret)

	errChan := make(chan error, 2)

	// Receiver node
	go func() {
		peer := NewTCPPeer(WithTCPPeerConn(serverConn), WithTCPPeerOutbound(false))
		errChan <- receiverHandshake(peer)
	}()

	// Initiator node
	clientPeer := NewTCPPeer(WithTCPPeerConn(clientConn), WithTCPPeerOutbound(true))
	initiatorErr := initiatorHandshake(clientPeer)
	receiverErr := <-errChan

	assert.NoError(t, initiatorErr)
	assert.NoError(t, receiverErr)
	assert.True(t, clientPeer.IsAuthenticated())
}

func TestMutualHMAC_Authentication_Rejected_WrongSecret(t *testing.T) {
	correctSecret := "valid-cluster-secret-12345"
	wrongSecret := "attacker-invalid-auth-key-fail"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	receiverHandshake := HMACClusterHandshake(correctSecret)
	attackerHandshake := HMACClusterHandshake(wrongSecret)

	errChan := make(chan error, 1)

	// Receiver node
	go func() {
		peer := NewTCPPeer(WithTCPPeerConn(serverConn), WithTCPPeerOutbound(false))
		errChan <- receiverHandshake(peer)
	}()

	// Attacker node attempting connection with wrong secret
	clientPeer := NewTCPPeer(WithTCPPeerConn(clientConn), WithTCPPeerOutbound(true))
	attackerErr := attackerHandshake(clientPeer)
	receiverErr := <-errChan

	// Both sides must detect authentication failure
	assert.Error(t, receiverErr)
	assert.Error(t, attackerErr)
	assert.False(t, clientPeer.IsAuthenticated())
}
