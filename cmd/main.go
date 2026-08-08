package main

import (
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/api"
	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

// killProcessOnPort kills any process listening on the specified port
func killProcessOnPort(port string) {
	// Find the PID using netstat and taskkill on Windows
	cmd := exec.Command("cmd", "/c", "for /f \"tokens=5\" %a in ('netstat -ano ^| findstr :"+port+"') do taskkill /PID %a /F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err := cmd.Run()
	if err != nil {
		// Try alternative approach using PowerShell
		cmd = exec.Command("powershell", "-command", "Get-Process -Id (Get-NetTCPConnection -LocalPort "+port+").OwningProcess | Stop-Process -Force")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		err = cmd.Run()
		if err != nil {
			log.Printf("Warning: Could not kill process on port %s: %v", port, err)
		} else {
			log.Printf("Killed process on port %s", port)
		}
	} else {
		log.Printf("Killed process on port %s", port)
	}
	// Give the system a moment to release the port
	time.Sleep(500 * time.Millisecond)
}

func makeServer(listenAddr string, encryptKey []byte, nodes ...string) *fileserver.FileServer {
	tcpTransport := p2p.NewTCPTransport(
		p2p.WithListenAddr(listenAddr),
		p2p.WithHandshakeFunc(p2p.NOPHandshakeFunc),
		p2p.WithDecoder(&p2p.DefaultDecoder{}),
	)

	fileServerOpts := fileserver.ServerOpts{
		EncryptKey:        encryptKey, // []byte matches the field type
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
	// crypto.NewEncryptionKey() returns []byte
	encryptKey, err := crypto.NewEncryptionKey()
	if err != nil {
		log.Fatal(err)
	}

	// Kill any existing processes on the ports we'll use
	killProcessOnPort("3000")
	killProcessOnPort("4000")
	killProcessOnPort("5000")
	killProcessOnPort("8080") // Frontend port

	s1 := makeServer(":3000", encryptKey, "")
	s2 := makeServer(":4000", encryptKey, "")
	s3 := makeServer(":5000", encryptKey, ":3000", ":4000")

	go func() {
		if err := s1.Start(); err != nil {
			log.Printf("Failed to start server 1: %v", err)
		}
	}()
	time.Sleep(1 * time.Second)

	go func() {
		if err := s2.Start(); err != nil {
			log.Printf("Failed to start server 2: %v", err)
		}
	}()
	time.Sleep(2 * time.Second)

	go func() {
		if err := s3.Start(); err != nil {
			log.Printf("Failed to start server 3: %v", err)
		}
	}()
	time.Sleep(2 * time.Second)

	// Serve the frontend (replaces the demo loop)
	webDir := filepath.Join(".", "web")
	fs := http.FileServer(http.Dir(webDir))

	app := api.New([]*fileserver.FileServer{s1, s2, s3})
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, app)

	mux.Handle("/", fs)
	log.Println("Frontend running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
