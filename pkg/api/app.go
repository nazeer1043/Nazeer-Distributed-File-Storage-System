package api

import (
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/db"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

// App holds application-wide dependencies.
type App struct {
	Servers   []*fileserver.FileServer
	DB        *db.DBWrapper
	Cfg       *config.Config
	StartTime time.Time
	rrIndex   uint64
}

// New creates a new App with the given file servers and database connection.
func New(servers []*fileserver.FileServer, database *db.DBWrapper) *App {
	return &App{
		Servers:   servers,
		DB:        database,
		StartTime: time.Now(),
	}
}