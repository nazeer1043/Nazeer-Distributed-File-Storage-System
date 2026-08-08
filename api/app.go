package api

import (
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

// App holds application-wide dependencies.
type App struct {
	Servers   []*fileserver.FileServer
	StartTime time.Time
}

// New creates a new App with the given file servers.
func New(servers []*fileserver.FileServer) *App {
	return &App{
		Servers:   servers,
		StartTime: time.Now(),
	}
}