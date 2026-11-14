package fileserver

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

func TestGDriveQueue_EnqueueAndMetadataStatusTracking(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gdrive_queue_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	key, err := crypto.NewEncryptionKey()
	require.NoError(t, err)

	transport := p2p.NewTCPTransport(
		p2p.WithListenAddr(":0"),
		p2p.WithHandshakeFunc(p2p.NOPHandshakeFunc),
		p2p.WithDecoder(p2p.NewFramedDecoder()),
	)

	opts := ServerOpts{
		StorageRoot:       tempDir,
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         transport,
		EncryptKey:        key,
	}

	server := NewFileServer(opts)
	defer server.Stop()

	// Initial metadata record
	fileMeta := &FileMeta{
		Key:      "cas-gdrive-test-key-1",
		Filename: "backup.txt",
		Size:     100,
		Owner:    "alice",
	}
	require.NoError(t, server.Metadata.Put(fileMeta))

	// 1. When GDrive is disabled, enqueue sets status to "disabled"
	server.GDrive.Enabled = false
	server.GDriveQueue.Enqueue(fileMeta.Key, fileMeta.Filename)

	m, ok := server.Metadata.Get(fileMeta.Key)
	require.True(t, ok)
	assert.Equal(t, "disabled", m.GDriveStatus)

	// 2. Metadata status updates
	err = server.Metadata.UpdateGDriveStatus(fileMeta.Key, "synced", "drive-id-998877", "")
	require.NoError(t, err)

	m2, ok := server.Metadata.Get(fileMeta.Key)
	require.True(t, ok)
	assert.Equal(t, "synced", m2.GDriveStatus)
	assert.Equal(t, "drive-id-998877", m2.GDriveFileID)

	// 3. Graceful shutdown of queue
	server.GDriveQueue.Stop()
	// Enqueue after stop should not panic
	server.GDriveQueue.Enqueue("another-key", "test.txt")
}
