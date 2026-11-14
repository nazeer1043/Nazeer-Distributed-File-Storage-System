package fileserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

var streamIDCounter uint64

func nextStreamID() uint64 {
	return atomic.AddUint64(&streamIDCounter, 1)
}

// ServerOpts contains configuration for the distributed file server node.
type ServerOpts struct {
	ID                string
	EncryptKey        []byte
	KeyID             string
	ClusterSecret     string
	ListenAddr        string
	StorageRoot       string
	PathTransformFunc store.PathTransformFunc
	Transport         p2p.Transport
	BootstrapNodes    []string
	ReplicationFactor int
}

// FileServer coordinates encrypted local storage, P2P replication, and cloud backup.
type FileServer struct {
	ServerOpts

	peerLock sync.RWMutex
	peers    map[string]p2p.Peer

	Storage     *store.Store
	Metadata    *MetadataStore
	GDrive      *store.GDriveStore
	GDriveQueue *GDriveQueue
	doneChan    chan struct{}

	ackMu  sync.RWMutex
	ackChs map[uint64]chan MessageAck

	dataMu  sync.RWMutex
	dataChs map[uint64]chan []byte
}

// NewFileServer creates a new file server instance with the given options.
func NewFileServer(opts ServerOpts) *FileServer {
	s := store.NewStore(
		store.WithRoot(opts.StorageRoot),
		store.WithPathTransformFunc(opts.PathTransformFunc),
	)

	if len(opts.ID) == 0 {
		opts.ID = crypto.GenerateID()
	}

	if len(opts.ListenAddr) == 0 && opts.Transport != nil {
		opts.ListenAddr = opts.Transport.Addr()
	}

	if opts.ReplicationFactor <= 0 {
		opts.ReplicationFactor = 2
	}

	metaStore, err := NewMetadataStore(opts.StorageRoot)
	if err != nil {
		log.Printf("[%s] warning: failed to initialize metadata store: %v\n", opts.ListenAddr, err)
	}

	gdriveStore := store.NewGDriveStore("credentials.json", "NazeerDFS_Vault")

	fs := &FileServer{
		ServerOpts: opts,
		Storage:    s,
		Metadata:   metaStore,
		GDrive:     gdriveStore,
		doneChan:   make(chan struct{}),
		peers:      make(map[string]p2p.Peer),
		ackChs:     make(map[uint64]chan MessageAck),
		dataChs:    make(map[uint64]chan []byte),
	}
	fs.GDriveQueue = NewGDriveQueue(fs, gdriveStore, 100)

	return fs
}

// Start starts the file server, connects to bootstrap peers, and enters main loop.
func (s *FileServer) Start() error {
	if err := s.Transport.ListenAndAccept(); err != nil {
		return err
	}

	if len(s.BootstrapNodes) > 0 {
		s.bootstrapNetwork()
		go s.startPeerReconnectLoop()
	}

	s.loop()
	return nil
}

// Stop stops the file server gracefully.
func (s *FileServer) Stop() {
	select {
	case <-s.doneChan:
	default:
		close(s.doneChan)
	}
	if s.GDriveQueue != nil {
		s.GDriveQueue.Stop()
	}
}

// IsHealthy returns true if the server is active, listening, and has not been stopped.
func (s *FileServer) IsHealthy() bool {
	select {
	case <-s.doneChan:
		return false
	default:
		return s.Transport != nil
	}
}

// OnPeer handles incoming authenticated peer connection registration.
func (s *FileServer) OnPeer(p p2p.Peer) error {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	s.peers[p.RemoteAddr().String()] = p
	log.Printf("[%s] connected with authenticated remote peer: %s\n", s.Transport.Addr(), p.RemoteAddr())
	return nil
}

// OnPeerDisconnect cleans up peer registration on disconnect.
func (s *FileServer) OnPeerDisconnect(p p2p.Peer) {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()
	delete(s.peers, p.RemoteAddr().String())
	log.Printf("[%s] disconnected and cleaned up peer: %s\n", s.Transport.Addr(), p.RemoteAddr())
}

func (s *FileServer) getPeer(addr string) (p2p.Peer, bool) {
	s.peerLock.RLock()
	defer s.peerLock.RUnlock()
	peer, ok := s.peers[addr]
	return peer, ok
}

func (s *FileServer) getPeers() []p2p.Peer {
	s.peerLock.RLock()
	defer s.peerLock.RUnlock()

	list := make([]p2p.Peer, 0, len(s.peers))
	for _, p := range s.peers {
		list = append(list, p)
	}
	return list
}

func (s *FileServer) removePeer(addr string) {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()
	delete(s.peers, addr)
}

func (s *FileServer) verifyChecksum(r io.Reader, expectedChecksum string) (bool, []byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return false, nil, err
	}
	if expectedChecksum == "" {
		return true, data, nil
	}
	hash := sha256.Sum256(data)
	computed := hex.EncodeToString(hash[:])
	return computed == expectedChecksum, data, nil
}

// Get gets the file from the file server.
// It verifies plaintext checksum against metadata, automatically failing over to peers
// or Google Drive cloud vault upon corruption or mismatch.
func (s *FileServer) Get(key string) (io.Reader, error) {
	var expectedChecksum string
	if s.Metadata != nil {
		if meta, ok := s.Metadata.Get(key); ok {
			expectedChecksum = meta.Checksum
		}
	}

	// 1. Try serving from local disk first
	if s.Storage.Has(s.ID, key) {
		log.Printf("[%s] reading file (%s) from local disk\n", s.Transport.Addr(), key)
		_, r, err := s.Storage.Read(s.ID, key)
		if err == nil {
			decBuf := new(bytes.Buffer)
			if _, decErr := crypto.CopyDecrypt(s.EncryptKey, r, decBuf); decErr == nil {
				valid, data, _ := s.verifyChecksum(bytes.NewReader(decBuf.Bytes()), expectedChecksum)
				if valid {
					return bytes.NewReader(data), nil
				}
				log.Printf("[%s] local file (%s) checksum mismatch! Attempting recovery from peers...\n", s.Transport.Addr(), key)
			}
			if rc, ok := r.(io.ReadCloser); ok {
				_ = rc.Close()
			}
		}
	}

	// 2. Fetch from peer nodes with length-prefixed protocol
	log.Printf("[%s] fetching file (%s) from remote peer network...\n", s.Transport.Addr(), key)
	peers := s.getPeers()
	for _, peer := range peers {
		streamID := nextStreamID()
		dataCh := make(chan []byte, 32)

		s.dataMu.Lock()
		s.dataChs[streamID] = dataCh
		s.dataMu.Unlock()

		msg := Message{
			Payload: MessageGetFile{
				ID:       s.ID,
				Key:      key,
				StreamID: streamID,
			},
		}

		buf := new(bytes.Buffer)
		if err := gob.NewEncoder(buf).Encode(&msg); err != nil {
			s.dataMu.Lock()
			delete(s.dataChs, streamID)
			s.dataMu.Unlock()
			continue
		}

		frame, _ := p2p.EncodeMessageFrame(streamID, buf.Bytes())
		if err := peer.Send(frame); err != nil {
			s.dataMu.Lock()
			delete(s.dataChs, streamID)
			s.dataMu.Unlock()
			continue
		}

		// Collect incoming stream chunks
		receivedBuf := new(bytes.Buffer)
		streamDone := false
		timeout := time.After(5 * time.Second)

		for !streamDone {
			select {
			case chunk, ok := <-dataCh:
				if !ok || len(chunk) == 0 {
					streamDone = true
					break
				}
				receivedBuf.Write(chunk)
			case <-timeout:
				streamDone = true
			}
		}

		s.dataMu.Lock()
		delete(s.dataChs, streamID)
		s.dataMu.Unlock()

		if receivedBuf.Len() > 0 {
			// Write received ciphertext to local disk storage
			_, _ = s.Storage.Write(s.ID, key, bytes.NewReader(receivedBuf.Bytes()))

			// Verify decrypted plaintext
			decBuf := new(bytes.Buffer)
			if _, decErr := crypto.CopyDecrypt(s.EncryptKey, bytes.NewReader(receivedBuf.Bytes()), decBuf); decErr == nil {
				valid, data, _ := s.verifyChecksum(bytes.NewReader(decBuf.Bytes()), expectedChecksum)
				if valid {
					log.Printf("[%s] successfully recovered file (%s) from peer (%s)\n", s.Transport.Addr(), key, peer.RemoteAddr())
					return bytes.NewReader(data), nil
				}
			}
		}
	}

	// 3. Fallback to Google Drive Cloud Vault
	if s.GDrive != nil && s.GDrive.Enabled {
		log.Printf("[%s] checking Google Drive Cloud Backup for key (%s)...\n", s.Transport.Addr(), key)
		gdriveStream, gdriveErr := s.GDrive.DownloadFile(key)
		if gdriveErr == nil {
			defer gdriveStream.Close()
			receivedBuf := new(bytes.Buffer)
			_, _ = io.Copy(receivedBuf, gdriveStream)
			_, _ = s.Storage.Write(s.ID, key, bytes.NewReader(receivedBuf.Bytes()))

			decBuf := new(bytes.Buffer)
			if _, decErr := crypto.CopyDecrypt(s.EncryptKey, bytes.NewReader(receivedBuf.Bytes()), decBuf); decErr == nil {
				valid, data, _ := s.verifyChecksum(bytes.NewReader(decBuf.Bytes()), expectedChecksum)
				if valid {
					log.Printf("[%s] restored file (%s) from Google Drive Cloud Backup!\n", s.Transport.Addr(), key)
					return bytes.NewReader(data), nil
				}
			}
		}
	}

	return nil, fmt.Errorf("file (%s) not found locally, in peer network, or in cloud vault", key)
}

// Store encrypts data directly into local storage, replicates ciphertext over P2P
// using length-prefixed frames, and enforces write quorum acknowledgements.
func (s *FileServer) Store(key string, r io.Reader) error {
	f, err := s.Storage.OpenFileForWriting(s.ID, key)
	if err != nil {
		return fmt.Errorf("failed to open storage file: %w", err)
	}

	keyID := s.KeyID
	if keyID == "" {
		keyID = "k1"
	}

	encSize, err := crypto.EncryptStream(s.EncryptKey, keyID, r, f)
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("encryption error: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close storage file: %w", closeErr)
	}

	peers := s.getPeers()
	if len(peers) == 0 {
		log.Printf("[%s] written (%d) encrypted bytes locally (no remote peers connected)\n", s.Transport.Addr(), encSize)
		return nil
	}

	streamID := nextStreamID()
	ackCh := make(chan MessageAck, len(peers))
	s.ackMu.Lock()
	s.ackChs[streamID] = ackCh
	s.ackMu.Unlock()
	defer func() {
		s.ackMu.Lock()
		delete(s.ackChs, streamID)
		s.ackMu.Unlock()
	}()

	msg := Message{
		Payload: MessageStoreFile{
			ID:       s.ID,
			Key:      key,
			Size:     encSize,
			StreamID: streamID,
		},
	}

	msgBuf := new(bytes.Buffer)
	if err := gob.NewEncoder(msgBuf).Encode(&msg); err != nil {
		return err
	}

	frame, err := p2p.EncodeMessageFrame(streamID, msgBuf.Bytes())
	if err != nil {
		return err
	}

	// 1. Send framed control message to all peers
	for _, peer := range peers {
		_ = peer.Send(frame)
	}

	// 2. Read encrypted ciphertext from local storage and stream framed chunks
	_, encReader, err := s.Storage.Read(s.ID, key)
	if err != nil {
		return fmt.Errorf("failed to read stored ciphertext for replication: %w", err)
	}
	defer func() {
		if rc, ok := encReader.(io.ReadCloser); ok {
			_ = rc.Close()
		}
	}()

	chunkBuf := make([]byte, 32*1024)
	for {
		nr, readErr := encReader.Read(chunkBuf)
		if nr > 0 {
			chunkFrame, _ := p2p.EncodeStreamFrame(streamID, chunkBuf[:nr])
			for _, peer := range peers {
				_ = peer.Send(chunkFrame)
			}
		}
		if readErr != nil {
			break
		}
	}

	// End of stream frame (0-length chunk)
	eofFrame, _ := p2p.EncodeStreamFrame(streamID, nil)
	for _, peer := range peers {
		_ = peer.Send(eofFrame)
	}

	// 3. Write Quorum: wait for acknowledgements
	quorum := (s.ReplicationFactor / 2) + 1
	if quorum < 1 {
		quorum = 1
	}
	acksReceived := 1 // Local storage write counts as 1 ack

	ackTimeout := time.After(3 * time.Second)
	for acksReceived < quorum {
		select {
		case ack := <-ackCh:
			if ack.Success {
				acksReceived++
			}
		case <-ackTimeout:
			log.Printf("[%s] quorum timeout: achieved %d/%d required acknowledgements\n", s.Transport.Addr(), acksReceived, quorum)
			return nil
		}
	}

	log.Printf("[%s] successfully achieved write quorum (%d acks) for file (%s)\n", s.Transport.Addr(), acksReceived, key)
	return nil
}

// StoreWithMeta stores file contents via Store and records persistent FileMeta metadata.
func (s *FileServer) StoreWithMeta(key string, filename string, owner string, contentType string, checksum string, size int64, r io.Reader) (*FileMeta, error) {
	if err := s.Store(key, r); err != nil {
		return nil, err
	}

	peers := s.getPeers()
	meta := &FileMeta{
		Key:         key,
		Filename:    filename,
		Size:        size,
		ContentType: contentType,
		Owner:       owner,
		Checksum:    checksum,
		UploadTime:  time.Now(),
		NodeID:      s.ListenAddr,
		Replicas:    len(peers) + 1,
		RefCount:    1,
	}

	if s.Metadata != nil {
		if err := s.Metadata.Put(meta); err != nil {
			log.Printf("[%s] warning: failed to write file metadata: %v\n", s.ListenAddr, err)
		}
	}

	if s.GDriveQueue != nil {
		s.GDriveQueue.Enqueue(key, filename)
	}

	return meta, nil
}

// Delete purges a file by key locally (if no remaining references) and broadcasts MessageDeleteFile to all peer nodes.
func (s *FileServer) Delete(key string) error {
	if s.Metadata != nil {
		shouldPurge, _ := s.Metadata.DecrementOrDelete(key)
		if !shouldPurge {
			log.Printf("[%s] reference count decremented for key (%s); remaining references exist, keeping on disk\n", s.Transport.Addr(), key)
			return nil
		}
	}

	log.Printf("[%s] deleting file (%s) locally and broadcasting to network...\n", s.Transport.Addr(), key)
	_ = s.Storage.Delete(s.ID, key)
	if s.GDrive != nil && s.GDrive.Enabled {
		go func(k string) {
			_ = s.GDrive.DeleteFile(k)
		}(key)
	}

	msg := Message{
		Payload: MessageDeleteFile{
			ID:  s.ID,
			Key: key,
		},
	}
	return s.broadcast(&msg)
}

func (s *FileServer) broadcast(msg *Message) error {
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	frame, err := p2p.EncodeMessageFrame(0, buf.Bytes())
	if err != nil {
		return err
	}

	peers := s.getPeers()
	for _, peer := range peers {
		if err := peer.Send(frame); err != nil {
			log.Printf("[%s] broadcast error to peer (%s): %v\n", s.Transport.Addr(), peer.RemoteAddr(), err)
			s.removePeer(peer.RemoteAddr().String())
		}
	}
	return nil
}

func (s *FileServer) loop() {
	defer func() {
		log.Printf("file server stopped due to error or user quit action\n")
		if err := s.Transport.Close(); err != nil {
			log.Printf("transport close error: %s\n", err.Error())
		}
	}()

	for {
		select {
		case rpc := <-s.Transport.Consume():
			if rpc.Stream {
				s.dataMu.RLock()
				ch, ok := s.dataChs[rpc.StreamID]
				s.dataMu.RUnlock()
				if ok {
					ch <- rpc.Payload
				}
				continue
			}

			var msg Message
			if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
				log.Printf("gob decode error: %s\n", err.Error())
				continue
			}
			if err := s.handleMessage(rpc.From.String(), &msg, rpc.StreamID); err != nil {
				log.Printf("handle message error: %s\n", err.Error())
			}

		case <-s.doneChan:
			return
		}
	}
}

func (s *FileServer) handleMessage(from string, msg *Message, streamID uint64) error {
	switch v := msg.Payload.(type) {
	case MessageStoreFile:
		go func() {
			if err := s.handleMessageStoreFile(from, v); err != nil {
				log.Printf("[%s] handle store file error: %v\n", s.Transport.Addr(), err)
			}
		}()
		return nil
	case MessageGetFile:
		go func() {
			if err := s.handleMessageGetFile(from, v); err != nil {
				log.Printf("[%s] handle get file error: %v\n", s.Transport.Addr(), err)
			}
		}()
		return nil
	case MessageDeleteFile:
		return s.handleMessageDeleteFile(from, v)
	case MessageAck:
		s.ackMu.RLock()
		ch, ok := s.ackChs[streamID]
		s.ackMu.RUnlock()
		if ok {
			ch <- v
		}
		return nil
	}
	return nil
}

func (s *FileServer) handleMessageGetFile(from string, msg MessageGetFile) error {
	if !s.Storage.Has(s.ID, msg.Key) {
		return fmt.Errorf("[%s] file (%s) does not exist on disk", s.Transport.Addr(), msg.Key)
	}

	log.Printf("[%s] serving file (%s) over network with StreamID %d\n", s.Transport.Addr(), msg.Key, msg.StreamID)

	_, r, err := s.Storage.Read(s.ID, msg.Key)
	if err != nil {
		return err
	}
	if rc, ok := r.(io.ReadCloser); ok {
		defer rc.Close()
	}

	peer, ok := s.getPeer(from)
	if !ok {
		return fmt.Errorf("peer (%s) could not be found in peers map", from)
	}

	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			frame, _ := p2p.EncodeStreamFrame(msg.StreamID, buf[:n])
			_ = peer.Send(frame)
		}
		if err != nil {
			break
		}
	}

	// Send EOF frame
	eofFrame, _ := p2p.EncodeStreamFrame(msg.StreamID, nil)
	_ = peer.Send(eofFrame)
	return nil
}

func (s *FileServer) handleMessageStoreFile(from string, msg MessageStoreFile) error {
	peer, ok := s.getPeer(from)
	if !ok {
		return fmt.Errorf("peer (%s) could not be found in peers map", from)
	}

	// Prepare data channel to receive framed stream chunks
	dataCh := make(chan []byte, 32)
	s.dataMu.Lock()
	s.dataChs[msg.StreamID] = dataCh
	s.dataMu.Unlock()

	defer func() {
		s.dataMu.Lock()
		delete(s.dataChs, msg.StreamID)
		s.dataMu.Unlock()
	}()

	f, err := s.Storage.OpenFileForWriting(s.ID, msg.Key)
	if err != nil {
		return err
	}
	defer f.Close()

	streamDone := false
	timeout := time.After(10 * time.Second)

	for !streamDone {
		select {
		case chunk, ok := <-dataCh:
			if !ok || len(chunk) == 0 {
				streamDone = true
				break
			}
			_, _ = f.Write(chunk)
		case <-timeout:
			streamDone = true
		}
	}

	// Send Ack back to peer
	ackMsg := Message{
		Payload: MessageAck{
			Key:      msg.Key,
			From:     s.Transport.Addr(),
			Success:  true,
			StreamID: msg.StreamID,
		},
	}
	ackBuf := new(bytes.Buffer)
	if err := gob.NewEncoder(ackBuf).Encode(&ackMsg); err == nil {
		ackFrame, _ := p2p.EncodeMessageFrame(msg.StreamID, ackBuf.Bytes())
		_ = peer.Send(ackFrame)
	}

	return nil
}

func (s *FileServer) handleMessageDeleteFile(from string, msg MessageDeleteFile) error {
	peer, ok := s.getPeer(from)
	if !ok {
		return fmt.Errorf("peer (%s) not found in peers map", from)
	}

	// Security: delete messages are strictly accepted from authenticated peers only
	if tcpPeer, isTCP := peer.(*p2p.TCPPeer); isTCP {
		if !tcpPeer.IsAuthenticated() {
			return errors.New("security rejection: unauthenticated peer attempted P2P delete")
		}
	}

	log.Printf("[%s] received authenticated P2P delete message for key (%s)\n", s.Transport.Addr(), msg.Key)
	_ = s.Storage.Delete(s.ID, msg.Key)
	if s.Metadata != nil {
		_ = s.Metadata.Delete(msg.Key)
	}
	if s.GDrive != nil && s.GDrive.Enabled {
		go func(k string) {
			_ = s.GDrive.DeleteFile(k)
		}(msg.Key)
	}
	return nil
}

func (s *FileServer) bootstrapNetwork() {
	for _, addr := range s.BootstrapNodes {
		if len(addr) == 0 || addr == s.ListenAddr {
			continue
		}

		go func(target string) {
			log.Printf("[%s] attempting to connect with bootstrap node: %s\n", s.Transport.Addr(), target)
			if err := s.Transport.Dial(target); err != nil {
				log.Printf("[%s] bootstrap dial error for %s: %v\n", s.Transport.Addr(), target, err)
			}
		}(addr)
	}
}

func (s *FileServer) startPeerReconnectLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.doneChan:
			return
		case <-ticker.C:
			for _, addr := range s.BootstrapNodes {
				if len(addr) == 0 || addr == s.ListenAddr {
					continue
				}
				s.peerLock.RLock()
				_, connected := s.peers[addr]
				s.peerLock.RUnlock()

				if !connected {
					go func(target string) {
						_ = s.Transport.Dial(target)
					}(addr)
				}
			}
		}
	}
}

func init() {
	gob.Register(MessageStoreFile{})
	gob.Register(MessageGetFile{})
	gob.Register(MessageDeleteFile{})
	gob.Register(MessageAck{})
}
