package handler

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/db"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/pkg/api"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

var (
	appInstance *api.App
	muxInstance *http.ServeMux
)

func init() {
	cfg, _ := config.LoadConfig()
	var encryptKey []byte
	if cfg != nil && len(cfg.EncryptionKey) == 32 {
		encryptKey = cfg.EncryptionKey
	} else {
		encryptKey, _ = crypto.NewEncryptionKey()
	}

	credsPath := "credentials.json"
	if cfg != nil && cfg.CredentialsPath != "" {
		credsPath = cfg.CredentialsPath
	}
	gdriveStore := store.NewGDriveStore(credsPath, store.DefaultVaultFolderName)

	tmpDir := os.TempDir()
	s1 := makeServerNode(":3000", filepath.Join(tmpDir, "3000_network"), encryptKey, gdriveStore)
	s2 := makeServerNode(":4000", filepath.Join(tmpDir, "4000_network"), encryptKey, gdriveStore)
	s3 := makeServerNode(":5000", filepath.Join(tmpDir, "5000_network"), encryptKey, gdriveStore)

	servers := []*fileserver.FileServer{s1, s2, s3}
	database := db.Connect(cfg)
	api.BootstrapAdmin(database, cfg)

	appInstance = api.New(servers, database)
	muxInstance = http.NewServeMux()
	api.RegisterRoutes(muxInstance, appInstance)
}

func makeServerNode(listenAddr string, storageRoot string, encryptKey []byte, gdriveStore *store.GDriveStore) *fileserver.FileServer {
	tcpTransport := p2p.NewTCPTransport(
		p2p.WithListenAddr(":0"),
		p2p.WithHandshakeFunc(p2p.NOPHandshakeFunc),
		p2p.WithDecoder(&p2p.DefaultDecoder{}),
	)

	fileServerOpts := fileserver.ServerOpts{
		ListenAddr:        listenAddr,
		EncryptKey:        encryptKey,
		StorageRoot:       storageRoot,
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         tcpTransport,
	}

	srv := fileserver.NewFileServer(fileServerOpts)
	srv.GDrive = gdriveStore
	return srv
}

// Handler is the Vercel serverless entry point.
func Handler(w http.ResponseWriter, r *http.Request) {
	muxInstance.ServeHTTP(w, r)
}
