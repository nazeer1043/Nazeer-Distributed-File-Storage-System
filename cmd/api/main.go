package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/db"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/pkg/api"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[API] Fatal configuration error: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("[API] Configuration validation failed: %v", err)
	}

	// Connect to MySQL database
	database := db.Connect(cfg)
	api.BootstrapAdmin(database, cfg)

	// Configure storage nodes pool from environment
	var servers []*fileserver.FileServer
	for i, addr := range cfg.NodeListenAddrs {
		storageRoot := cfg.StorageRoots[i]
		var bootstraps []string
		if i > 0 {
			bootstraps = cfg.BootstrapNodes
		}

		transport := p2p.NewTCPTransport(
			p2p.WithListenAddr(addr),
			p2p.WithHandshakeFunc(p2p.HMACClusterHandshake(cfg.ClusterSecret)),
			p2p.WithDecoder(p2p.NewFramedDecoder()),
		)

		s := fileserver.NewFileServer(fileserver.ServerOpts{
			ListenAddr:        addr,
			EncryptKey:        cfg.EncryptionKey,
			StorageRoot:       storageRoot,
			PathTransformFunc: store.CASPathTransformFunc,
			Transport:         transport,
			BootstrapNodes:    bootstraps,
			ClusterSecret:     cfg.ClusterSecret,
			ReplicationFactor: 2,
		})
		transport.OnPeer = s.OnPeer
		transport.OnPeerDisconnect = s.OnPeerDisconnect
		servers = append(servers, s)
	}

	// Start storage nodes
	for i, srv := range servers {
		go func(s *fileserver.FileServer, index int) {
			if err := s.Start(); err != nil {
				log.Printf("[API] Storage server %d (%s) exited: %v", index+1, s.ListenAddr, err)
			}
		}(srv, i)
	}

	// Initialize App and routes
	app := api.New(servers, database)
	app.Cfg = cfg

	mux := http.NewServeMux()
	api.RegisterRoutes(mux, app)

	webDir := filepath.Join(".", "web")
	fs := http.FileServer(http.Dir(webDir))

	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Clean("/" + r.URL.Path)
		if path == "/" {
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
			return
		}
		cleanPath := filepath.Join(webDir, strings.TrimPrefix(path, "/"))
		rel, err := filepath.Rel(webDir, cleanPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
			htmlCandidate := cleanPath + ".html"
			if _, errHTML := os.Stat(htmlCandidate); errHTML == nil {
				http.ServeFile(w, r, htmlCandidate)
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
	mux.Handle("/", staticHandler)

	secureHandler := api.StructuredLoggingMiddleware(api.SecurityHeadersMiddleware(api.CSRFMiddleware(mux)))

	httpPort := cfg.HTTPPort
	if p := os.Getenv("PORT"); p != "" {
		httpPort = p
	}

	httpServer := &http.Server{
		Addr:              ":" + httpPort,
		Handler:           secureHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[API] NazeerDFS Web UI & REST API listening at http://localhost:%s\n", httpPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[API] HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("[API] Shutting down NazeerDFS HTTP API server cleanly...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("[API] HTTP shutdown error: %v", err)
	}

	for _, s := range servers {
		s.Stop()
	}
	log.Println("[API] Shutdown complete.")
}
