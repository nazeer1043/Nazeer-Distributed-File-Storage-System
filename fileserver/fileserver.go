package fileserver

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

// ServerOpts is a struct that contains the configuration for the file server.
type ServerOpts struct {
	ID                string
	EncryptKey        []byte
	ListenAddr        string
	StorageRoot       string
	PathTransformFunc store.PathTransformFunc
	Transport         p2p.Transport
	BootstrapNodes    []string
}

// FileServer is a struct that contains the configuration for the file server.
type FileServer struct {
	ServerOpts

	peerLock sync.RWMutex
	peers    map[string]p2p.Peer

	Storage  *store.Store
	Metadata *MetadataStore
	GDrive   *store.GDriveStore
	doneChan chan struct{}
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

	metaStore, err := NewMetadataStore(opts.StorageRoot)
	if err != nil {
		log.Printf("[%s] warning: failed to initialize metadata store: %v\n", opts.ListenAddr, err)
	}

	gdriveStore := store.NewGDriveStore("credentials.json", "NazeerDFS_Vault")

	return &FileServer{
		ServerOpts: opts,
		Storage:    s,
		Metadata:   metaStore,
		GDrive:     gdriveStore,
		doneChan:   make(chan struct{}),
		peers:      make(map[string]p2p.Peer),
	}
}

// Start starts the file server.
func (s *FileServer) Start() error {
	if err := s.Transport.ListenAndAccept(); err != nil {
		return err
	}

	if len(s.BootstrapNodes) > 0 {
		s.bootstrapNetwork()
	}

	s.loop()

	return nil
}

// Stop stops the file server.
func (s *FileServer) Stop() {
	close(s.doneChan)
}

// OnPeer is a callback function that is called when a peer is connected to the file server.
func (s *FileServer) OnPeer(p p2p.Peer) error {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	s.peers[p.RemoteAddr().String()] = p

	log.Printf("[%s] connected with remote peer: %s\n", s.Transport.Addr(), p.RemoteAddr())

	return nil
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

// Get gets the data from the file server.
// It reads the data from the store if it exists, otherwise it fetches the data from the network.
func (s *FileServer) Get(key string) (io.Reader, error) {
	if s.Storage.Has(s.ID, key) {
		log.Printf("[%s] serving file (%s) from local disk\n", s.Transport.Addr(), key)
		_, r, err := s.Storage.Read(s.ID, key)
		return r, err
	}

	log.Printf("[%s] dont have the file (%s) locally, fetching from network...\n", s.Transport.Addr(), key)

	msg := Message{
		Payload: MessageGetFile{
			ID:  s.ID,
			Key: crypto.HashKey(key),
		},
	}

	if err := s.broadcast(&msg); err != nil {
		return nil, err
	}

	time.Sleep(time.Millisecond * 100)

	peers := s.getPeers()
	var fetchedOverP2P bool
	for _, peer := range peers {
		var fileSize int64
		if err := binary.Read(peer, binary.LittleEndian, &fileSize); err != nil {
			log.Printf("[%s] peer read error from (%s): %v\n", s.Transport.Addr(), peer.RemoteAddr(), err)
			continue
		}

		n, err := s.Storage.WriteDecrypt(
			s.EncryptKey,
			s.ID,
			key,
			io.LimitReader(peer, fileSize),
		)
		if err != nil {
			log.Printf("[%s] decrypt write error from (%s): %v\n", s.Transport.Addr(), peer.RemoteAddr(), err)
			continue
		}

		log.Printf("[%s] received (%d) bytes over the network from (%s)\n", s.Transport.Addr(), n, peer.RemoteAddr())
		peer.CloseStream()
		fetchedOverP2P = true
		break
	}

	if !fetchedOverP2P && s.GDrive != nil && s.GDrive.Enabled {
		log.Printf("[%s] checking Google Drive 400GB Cloud Backup for key (%s)...\n", s.Transport.Addr(), key)
		gdriveStream, gdriveErr := s.GDrive.DownloadFile(key)
		if gdriveErr == nil {
			n, writeErr := s.Storage.WriteDecrypt(s.EncryptKey, s.ID, key, gdriveStream)
			_ = gdriveStream.Close()
			if writeErr == nil {
				log.Printf("[%s] restored file (%s) [%d bytes] from Google Drive Cloud Backup!\n", s.Transport.Addr(), key, n)
			} else {
				log.Printf("[%s] GDrive decrypt write error: %v\n", s.Transport.Addr(), writeErr)
			}
		} else {
			log.Printf("[%s] GDrive download lookup error: %v\n", s.Transport.Addr(), gdriveErr)
		}
	}

	_, r, err := s.Storage.Read(s.ID, key)
	return r, err
}

// Store stores the data in the file server.
// It writes the data to the store and then broadcasts the message to the peers.
func (s *FileServer) Store(key string, r io.Reader) error {
	var (
		fileBuffer = new(bytes.Buffer)
		tee        = io.TeeReader(r, fileBuffer)
	)

	size, err := s.Storage.Write(s.ID, key, tee)
	if err != nil {
		return err
	}

	msg := Message{
		Payload: MessageStoreFile{
			ID:   s.ID,
			Key:  crypto.HashKey(key),
			Size: size + 16,
		},
	}

	if err = s.broadcast(&msg); err != nil {
		return err
	}

	time.Sleep(time.Millisecond * 5)

	peers := s.getPeers()
	if len(peers) == 0 {
		log.Printf("[%s] written (%d) bytes locally (no remote peers connected)\n", s.Transport.Addr(), size)
		return nil
	}

	writers := make([]io.Writer, 0, len(peers))
	for _, peer := range peers {
		writers = append(writers, peer)
	}

	mw := io.MultiWriter(writers...)
	_, _ = mw.Write([]byte{p2p.IncomingStream})

	n, err := crypto.CopyEncrypt(s.EncryptKey, fileBuffer, mw)
	if err != nil {
		log.Printf("[%s] warning: peer replication error: %v (file stored locally and queued for GDrive)\n", s.Transport.Addr(), err)
	} else {
		log.Printf("[%s] written (%d) bytes locally and replicated to %d peers\n", s.Transport.Addr(), n, len(peers))
	}

	return nil
}

// StoreWithMeta stores file contents via Store and records persistent FileMeta metadata.
func (s *FileServer) StoreWithMeta(key string, filename string, owner string, contentType string, r io.Reader) (*FileMeta, error) {
	// Read full payload into memory so it can be written locally, replicated over P2P, and backed up to GDrive
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read upload payload: %w", err)
	}

	if err := s.Store(key, bytes.NewReader(data)); err != nil {
		return nil, err
	}

	peers := s.getPeers()
	meta := &FileMeta{
		Key:         key,
		Filename:    filename,
		Size:        int64(len(data)),
		ContentType: contentType,
		Owner:       owner,
		Checksum:    crypto.HashKey(key),
		UploadTime:  time.Now(),
		NodeID:      s.ListenAddr,
		Replicas:    len(peers) + 1,
	}

	if s.Metadata != nil {
		if err := s.Metadata.Put(meta); err != nil {
			log.Printf("[%s] warning: failed to write file metadata: %v\n", s.ListenAddr, err)
		}
	}

	if s.GDrive != nil && s.GDrive.Enabled {
		encBuf := new(bytes.Buffer)
		if _, encErr := crypto.CopyEncrypt(s.EncryptKey, bytes.NewReader(data), encBuf); encErr == nil {
			if driveFile, gdriveErr := s.GDrive.UploadFile(key, filename, encBuf); gdriveErr == nil {
				log.Printf("[%s] Google Drive Cloud Backup upload successful for (%s)! Drive File ID: %s\n", s.ListenAddr, filename, driveFile.Id)
			} else {
				log.Printf("[%s] GDrive upload error for (%s): %v\n", s.ListenAddr, filename, gdriveErr)
			}
		} else {
			log.Printf("[%s] GDrive encryption error: %v\n", s.ListenAddr, encErr)
		}
	}

	return meta, nil
}

func (s *FileServer) broadcast(msg *Message) error {
	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(msg); err != nil {
		return err
	}

	peers := s.getPeers()
	for _, peer := range peers {
		_ = peer.Send([]byte{p2p.IncomingMessage})
		if err := peer.Send(buf.Bytes()); err != nil {
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
			var msg Message
			if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
				log.Printf("gob decode error: %s\n", err.Error())
				continue
			}
			if err := s.handleMessage(rpc.From.String(), &msg); err != nil {
				log.Printf("handle message error: %s\n", err.Error())
			}

		case <-s.doneChan:
			return
		}
	}
}

func (s *FileServer) handleMessage(from string, msg *Message) error {
	switch v := msg.Payload.(type) {
	case MessageStoreFile:
		return s.handleMessageStoreFile(from, v)
	case MessageGetFile:
		return s.handleMessageGetFile(from, v)
	case MessageDeleteFile:
		return s.handleMessageDeleteFile(from, v)
	}
	return nil
}

func (s *FileServer) handleMessageGetFile(from string, msg MessageGetFile) error {
	if !s.Storage.Has(msg.ID, msg.Key) {
		return fmt.Errorf("[%s] need to serve file (%s) but it does not exist on disk", s.Transport.Addr(), msg.Key) //nolint:err113
	}

	log.Printf("[%s] serving file (%s) over the network\n", s.Transport.Addr(), msg.Key)

	fileSize, r, err := s.Storage.Read(msg.ID, msg.Key)
	if err != nil {
		return err
	}

	if rc, ok := r.(io.ReadCloser); ok {
		defer func() { _ = rc.Close() }()
	}

	peer, ok := s.getPeer(from)
	if !ok {
		return fmt.Errorf("peer (%s) could not be found in the peers map", from) //nolint:err113
	}

	// First send the "incomingStream" byte to the peer then
	// we can send the file size as an int64.
	_ = peer.Send([]byte{p2p.IncomingStream})
	if wErr := binary.Write(peer, binary.LittleEndian, fileSize); wErr != nil {
		return wErr
	}
	n, err := io.Copy(peer, r)
	if err != nil {
		return err
	}

	log.Printf("[%s] written (%d) bytes over the network to %s\n", s.Transport.Addr(), n, from)

	return nil
}

func (s *FileServer) handleMessageStoreFile(from string, msg MessageStoreFile) error {
	peer, ok := s.getPeer(from)
	if !ok {
		return fmt.Errorf("peer (%s) could not be found in the peers map", from) //nolint:err113
	}

	n, err := s.Storage.Write(msg.ID, msg.Key, io.LimitReader(peer, msg.Size))
	if err != nil {
		return err
	}
	log.Printf("[%s] written %d bytes to disk\n", s.Transport.Addr(), n)

	peer.CloseStream()

	return nil
}

func (s *FileServer) handleMessageDeleteFile(from string, msg MessageDeleteFile) error {
	log.Printf("[%s] received P2P delete message for key (%s)\n", s.Transport.Addr(), msg.Key)
	_ = s.Storage.Delete(msg.ID, msg.Key)
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
		if len(addr) == 0 {
			continue
		}

		go func(addr string) {
			log.Printf("[%s] attempting to connect with remote: %s\n", s.Transport.Addr(), addr)
			if err := s.Transport.Dial(addr); err != nil {
				log.Printf("dial error: %s\n", err.Error())
			}
		}(addr)
	}
}

func init() {
	gob.Register(MessageStoreFile{})
	gob.Register(MessageGetFile{})
	gob.Register(MessageDeleteFile{})
}
