package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[Node] Fatal configuration error: %v", err)
	}

	listenAddr := os.Getenv("NODE_ADDR")
	if listenAddr == "" {
		listenAddr = os.Getenv("PORT")
	}
	if listenAddr == "" {
		if len(cfg.NodeListenAddrs) > 0 {
			listenAddr = cfg.NodeListenAddrs[0]
		} else {
			listenAddr = ":3000"
		}
	}
	if !strings.Contains(listenAddr, ":") {
		listenAddr = ":" + listenAddr
	}

	storageRoot := os.Getenv("STORAGE_ROOT")
	if storageRoot == "" {
		if len(cfg.StorageRoots) > 0 {
			storageRoot = cfg.StorageRoots[0]
		} else {
			storageRoot = filepath.Join("data", strings.ReplaceAll(strings.TrimPrefix(listenAddr, ":"), "/", "_")+"_network")
		}
	}

	bootstrapStr := os.Getenv("BOOTSTRAP_NODES")
	var bootstraps []string
	if bootstrapStr != "" {
		for _, b := range strings.Split(bootstrapStr, ",") {
			b = strings.TrimSpace(b)
			if b != "" {
				bootstraps = append(bootstraps, b)
			}
		}
	} else if len(cfg.BootstrapNodes) > 0 {
		bootstraps = cfg.BootstrapNodes
	}

	replFactor := 2
	if rfStr := os.Getenv("REPLICATION_FACTOR"); rfStr != "" {
		if rf, err := strconv.Atoi(rfStr); err == nil && rf > 0 {
			replFactor = rf
		}
	}

	clusterSecret := cfg.ClusterSecret
	if clusterSecret == "" {
		clusterSecret = "nazeerdfs_cluster_secret_fallback_key"
	}

	transport := p2p.NewTCPTransport(
		p2p.WithListenAddr(listenAddr),
		p2p.WithHandshakeFunc(p2p.HMACClusterHandshake(clusterSecret)),
		p2p.WithDecoder(p2p.NewFramedDecoder()),
	)

	opts := fileserver.ServerOpts{
		ListenAddr:        listenAddr,
		EncryptKey:        cfg.EncryptionKey,
		StorageRoot:       storageRoot,
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         transport,
		BootstrapNodes:    bootstraps,
		ClusterSecret:     clusterSecret,
		ReplicationFactor: replFactor,
	}

	server := fileserver.NewFileServer(opts)
	transport.OnPeer = server.OnPeer
	transport.OnPeerDisconnect = server.OnPeerDisconnect

	log.Printf("[Node] Starting NazeerDFS Storage Node on %s (storage root: %s, bootstraps: %v)...\n",
		listenAddr, storageRoot, bootstraps)

	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("[Node] Fatal storage node error: %v", err)
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	<-stopChan

	log.Println("[Node] Shutting down storage node gracefully...")
	server.Stop()
	log.Println("[Node] Storage node stopped.")
}
