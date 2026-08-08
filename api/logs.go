package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

func (a *App) LogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	now := time.Now().Format("15:04:05")
	logs := []LogEntry{
		{Timestamp: now, Level: "INFO", Message: "raft: leader election completed on node-a1"},
		{Timestamp: now, Level: "INFO", Message: "shard[0x4f2a] replicated to node-b2, node-c3"},
		{Timestamp: now, Level: "WARN", Message: "gc pause 182ms on node-b2, above threshold"},
		{Timestamp: now, Level: "INFO", Message: "heartbeat ack from node-c3 (rtt 4ms)"},
		{Timestamp: now, Level: "INFO", Message: "chunk store compaction finished, freed 2.1GB"},
		{Timestamp: now, Level: "INFO", Message: "metadata checkpoint written to node-b2"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}
