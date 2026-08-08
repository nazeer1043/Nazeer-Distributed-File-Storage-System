package api

import (
	"encoding/json"
	"net/http"
)

type NodeInfo struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Addr      string  `json:"addr"`
	Status    string  `json:"status"`
	Storage   string  `json:"storage"`
	Used      float64 `json:"used"`
	Total     float64 `json:"total"`
	Role      string  `json:"role"`
	Uptime    string  `json:"uptime"`
	Latency   string  `json:"latency"`
	Replicas  int     `json:"replicas"`
}

func (a *App) NodesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodes := []NodeInfo{
		{ID: "node-3000", Name: "Node 3000 (Bootstrap)", Addr: ":3000", Status: "Online", Storage: "4.2 TB / 4.0 TB", Used: 4.2, Total: 4.0, Role: "Bootstrap Leader", Uptime: "99.99%", Latency: "2ms", Replicas: 128},
		{ID: "node-4000", Name: "Node 4000 (Peer)", Addr: ":4000", Status: "Online", Storage: "2.1 TB / 4.0 TB", Used: 2.1, Total: 4.0, Role: "Storage Peer", Uptime: "99.95%", Latency: "4ms", Replicas: 96},
		{ID: "node-5000", Name: "Node 5000 (Peer)", Addr: ":5000", Status: "Online", Storage: "1.0 TB / 4.0 TB", Used: 1.0, Total: 4.0, Role: "Storage Peer", Uptime: "100.0%", Latency: "3ms", Replicas: 64},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
