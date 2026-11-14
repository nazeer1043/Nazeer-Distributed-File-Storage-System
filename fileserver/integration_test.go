package fileserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

func TestMultiNode_StoreNodeA_KillA_ReadNodeB_Integrity(t *testing.T) {
	clusterSecret := "secure-integration-test-cluster-secret-2026"
	encKey, err := crypto.NewEncryptionKey()
	require.NoError(t, err)

	tempDirA, err := os.MkdirTemp("", "nodeA_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDirA)

	tempDirB, err := os.MkdirTemp("", "nodeB_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDirB)

	// 1. Create Transport for Node A
	transportA := p2p.NewTCPTransport(
		p2p.WithListenAddr("127.0.0.1:0"),
		p2p.WithHandshakeFunc(p2p.HMACClusterHandshake(clusterSecret)),
		p2p.WithDecoder(p2p.NewFramedDecoder()),
	)

	// 2. Create Transport for Node B
	transportB := p2p.NewTCPTransport(
		p2p.WithListenAddr("127.0.0.1:0"),
		p2p.WithHandshakeFunc(p2p.HMACClusterHandshake(clusterSecret)),
		p2p.WithDecoder(p2p.NewFramedDecoder()),
	)

	// 3. Create Node A
	optsA := ServerOpts{
		StorageRoot:       tempDirA,
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         transportA,
		EncryptKey:        encKey,
		ClusterSecret:     clusterSecret,
		ReplicationFactor: 2,
	}
	nodeA := NewFileServer(optsA)
	transportA.OnPeer = nodeA.OnPeer
	transportA.OnPeerDisconnect = nodeA.OnPeerDisconnect

	// 4. Create Node B
	optsB := ServerOpts{
		StorageRoot:       tempDirB,
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         transportB,
		EncryptKey:        encKey,
		ClusterSecret:     clusterSecret,
		ReplicationFactor: 2,
	}
	nodeB := NewFileServer(optsB)
	transportB.OnPeer = nodeB.OnPeer
	transportB.OnPeerDisconnect = nodeB.OnPeerDisconnect

	// Start transports
	require.NoError(t, transportA.ListenAndAccept())
	require.NoError(t, transportB.ListenAndAccept())

	nodeA.ListenAddr = transportA.Addr()
	nodeB.ListenAddr = transportB.Addr()

	go nodeA.loop()
	go nodeB.loop()

	defer nodeA.Stop()
	defer nodeB.Stop()

	// Connect Node B to Node A
	require.NoError(t, transportB.Dial(transportA.Addr()))

	// Wait for handshake and peer registration to complete
	require.Eventually(t, func() bool {
		return len(nodeA.getPeers()) >= 1 && len(nodeB.getPeers()) >= 1
	}, 5*time.Second, 50*time.Millisecond, "Nodes must complete mutual authentication and register peers")

	// 5. Store file on Node A
	fileContent := []byte("Distributed content stored on Node A and replicated to Node B with write quorum")
	hash := sha256.Sum256(fileContent)
	checksum := hex.EncodeToString(hash[:])
	fileKey := "cas-integration-key-alpha-bravo-12345"

	meta, err := nodeA.StoreWithMeta(fileKey, "distributed.txt", "alice", "text/plain", checksum, int64(len(fileContent)), bytes.NewReader(fileContent))
	require.NoError(t, err)
	assert.Equal(t, fileKey, meta.Key)

	// Propagate metadata to Node B for checksum verification
	_ = nodeB.Metadata.Put(meta)

	// 6. Kill Node A (stop server and close listener)
	nodeA.Stop()
	_ = transportA.Close()

	// 7. Read file from Node B
	reader, err := nodeB.Get(fileKey)
	require.NoError(t, err, "Node B must serve file after Node A is killed")

	readContent, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, fileContent, readContent, "Retrieved data from Node B must match original payload exactly")

	// 8. Security verification: Unauthenticated peer connection is rejected
	peersBefore := len(nodeB.getPeers())
	unauthenticatedConn, dialErr := net.DialTimeout("tcp", transportB.Addr(), 2*time.Second)
	require.NoError(t, dialErr)
	// Write 64 bytes of invalid cryptographic challenge response
	_, _ = unauthenticatedConn.Write(make([]byte, 64))
	// Node B must reject handshake and close connection
	drainBuf := make([]byte, 128)
	var lastErr error
	for {
		_ = unauthenticatedConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, rErr := unauthenticatedConn.Read(drainBuf)
		if rErr != nil {
			lastErr = rErr
			break
		}
	}
	assert.Error(t, lastErr, "Node B must terminate connection on invalid handshake")
	_ = unauthenticatedConn.Close()
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, peersBefore, len(nodeB.getPeers()), "Unauthenticated peer must not be added to peer roster")
}
