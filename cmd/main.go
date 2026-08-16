package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/api"
	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/db"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

// killProcessOnPort kills any process listening on the specified port (Windows local dev only)
func killProcessOnPort(port string) {
	if runtime.GOOS != "windows" {
		return
	}
	cmd := exec.Command("cmd", "/c", "for /f \"tokens=5\" %a in ('netstat -ano ^| findstr :"+port+"') do taskkill /PID %a /F")
	err := cmd.Run()
	if err != nil {
		cmd = exec.Command("powershell", "-command", "Get-Process -Id (Get-NetTCPConnection -LocalPort "+port+").OwningProcess | Stop-Process -Force")
		err = cmd.Run()
		if err != nil {
			log.Printf("Warning: Could not kill process on port %s: %v", port, err)
		} else {
			log.Printf("Killed process on port %s", port)
		}
	} else {
		log.Printf("Killed process on port %s", port)
	}
	time.Sleep(500 * time.Millisecond)
}

func makeServer(listenAddr string, encryptKey []byte, nodes ...string) *fileserver.FileServer {
	tcpTransport := p2p.NewTCPTransport(
		p2p.WithListenAddr(listenAddr),
		p2p.WithHandshakeFunc(p2p.NOPHandshakeFunc),
		p2p.WithDecoder(&p2p.DefaultDecoder{}),
	)

	fileServerOpts := fileserver.ServerOpts{
		EncryptKey:        encryptKey,
		StorageRoot:       strings.TrimPrefix(listenAddr, ":") + "_network",
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    nodes,
	}

	s := fileserver.NewFileServer(fileServerOpts)
	tcpTransport.OnPeer = s.OnPeer
	return s
}

func main() {
	cfg := config.LoadConfig()

	encryptKey, err := crypto.NewEncryptionKey()
	if err != nil {
		log.Fatal(err)
	}

	// Kill any existing processes on the ports we'll use
	for _, port := range append(cfg.NodePorts, cfg.HTTPPort) {
		killProcessOnPort(port)
	}

	s1 := makeServer(":"+cfg.NodePorts[0], encryptKey, "")
	s2 := makeServer(":"+cfg.NodePorts[1], encryptKey, "")
	s3 := makeServer(":"+cfg.NodePorts[2], encryptKey, ":"+cfg.NodePorts[0], ":"+cfg.NodePorts[1])

	servers := []*fileserver.FileServer{s1, s2, s3}
	for i, srv := range servers {
		go func(s *fileserver.FileServer, index int) {
			if err := s.Start(); err != nil {
				log.Printf("Failed to start server %d: %v", index+1, err)
			}
		}(srv, i)
		time.Sleep(1 * time.Second)
	}

	// Connect to MySQL database
	database := db.Connect(cfg)

	// Serve the web frontend and API endpoints
	webDir := filepath.Join(".", "web")
	fs := http.FileServer(http.Dir(webDir))

	app := api.New(servers, database)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, app)

	mux.Handle("/", fs)

	httpServer := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: mux,
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

