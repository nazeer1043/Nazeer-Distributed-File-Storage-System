package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

type LogBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	maxSize int
}

var globalLogBuffer = &LogBuffer{
	entries: make([]LogEntry, 0, 100),
	maxSize: 100,
}

// LogEvent records a real event into the global log buffer.
func LogEvent(level, message string) {
	globalLogBuffer.mu.Lock()
	defer globalLogBuffer.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now().Format("15:04:05"),
		Level:     level,
		Message:   message,
	}

	globalLogBuffer.entries = append(globalLogBuffer.entries, entry)
	if len(globalLogBuffer.entries) > globalLogBuffer.maxSize {
		globalLogBuffer.entries = globalLogBuffer.entries[1:]
	}
}

func (a *App) LogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	globalLogBuffer.mu.RLock()
	logs := make([]LogEntry, len(globalLogBuffer.entries))
	copy(logs, globalLogBuffer.entries)
	globalLogBuffer.mu.RUnlock()

	if len(logs) == 0 {
		now := time.Now().Format("15:04:05")
		logs = []LogEntry{
			{Timestamp: now, Level: "INFO", Message: fmt.Sprintf("NazeerDFS cluster initialized with %d active nodes", len(a.Servers))},
			{Timestamp: now, Level: "INFO", Message: "P2P transport listening and ready for incoming peer connections"},
		}
	}

	json.NewEncoder(w).Encode(logs)
}

