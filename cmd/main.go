package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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

// killProcessOnPort kills any process listening on the specified port (Windows local dev only)
func killProcessOnPort(port string) {
	if runtime.GOOS != "windows" {
		return
	}
	// Check if port is already free
	ln, err := net.Listen("tcp", ":"+port)
	if err == nil {
		_ = ln.Close()
		return
	}

	cmd := exec.Command("cmd", "/c", fmt.Sprintf("for /f \"tokens=5\" %%a in ('netstat -ano ^| findstr /r \":%s \"') do taskkill /PID %%a /F", port))
	_ = cmd.Run()
	time.Sleep(100 * time.Millisecond)
}

func makeServer(listenAddr string, storageRoot string, encryptKey []byte, nodes ...string) *fileserver.FileServer {
	tcpTransport := p2p.NewTCPTransport(
		p2p.WithListenAddr(listenAddr),
		p2p.WithHandshakeFunc(p2p.NOPHandshakeFunc),
		p2p.WithDecoder(&p2p.DefaultDecoder{}),
	)

	fileServerOpts := fileserver.ServerOpts{
		ListenAddr:        listenAddr,
		EncryptKey:        encryptKey,
		StorageRoot:       storageRoot,
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    nodes,
	}

	s := fileserver.NewFileServer(fileServerOpts)
	tcpTransport.OnPeer = s.OnPeer
	return s
}

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Fatal configuration error: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	encryptKey := cfg.EncryptionKey

	// Kill any existing processes on the ports we'll use (windows dev only)
	var allPorts []string
	for _, addr := range cfg.NodeListenAddrs {
		allPorts = append(allPorts, strings.TrimPrefix(addr, ":"))
	}
	allPorts = append(allPorts, cfg.HTTPPort)
	for _, port := range allPorts {
		killProcessOnPort(port)
	}

	var servers []*fileserver.FileServer
	for i, addr := range cfg.NodeListenAddrs {
		storageRoot := cfg.StorageRoots[i]
		var bootstraps []string
		if i > 0 {
			bootstraps = cfg.BootstrapNodes
		}
		s := makeServer(addr, storageRoot, encryptKey, bootstraps...)
		servers = append(servers, s)
	}

	for i, srv := range servers {
		go func(s *fileserver.FileServer, index int) {
			if err := s.Start(); err != nil {
				log.Printf("Failed to start server %d: %v", index+1, err)
			}
		}(srv, i)
		time.Sleep(200 * time.Millisecond)
	}

	// Connect to MySQL database
	database := db.Connect(cfg)
	api.BootstrapAdmin(database, cfg)

	// Serve the web frontend and API endpoints
	webDir := filepath.Join(".", "web")
	fs := http.FileServer(http.Dir(webDir))

	app := api.New(servers, database)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, app)

	// Clean static router: serves index.html at root and matches clean routes (e.g. /dashboard -> dashboard.html)
	staticHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
			return
		}
		cleanPath := filepath.Join(webDir, strings.TrimPrefix(path, "/"))
		if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
			if _, errHTML := os.Stat(cleanPath + ".html"); errHTML == nil {
				http.ServeFile(w, r, cleanPath+".html")
				return
			}
		}
		fs.ServeHTTP(w, r)
	})

	mux.Handle("/", staticHandler)

	// Apply defense-in-depth security headers and anti-CSRF token verification
	secureHandler := api.SecurityHeadersMiddleware(api.CSRFMiddleware(mux))

	httpServer := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: secureHandler,
	}

	// Graceful shutdown listener
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Frontend & REST API running at http://localhost:%s\n", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("\nShutting down NazeerDFS cluster cleanly...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v\n", err)
	}

	for _, s := range servers {
		s.Stop()
	}

	log.Println("NazeerDFS cluster shutdown complete.")
}

