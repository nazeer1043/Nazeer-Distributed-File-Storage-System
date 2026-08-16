package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type NodeInfo struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Addr     string  `json:"addr"`
	Status   string  `json:"status"`
	Storage  string  `json:"storage"`
	Used     float64 `json:"used"`
	Total    float64 `json:"total"`
	Role     string  `json:"role"`
	Uptime   string  `json:"uptime"`
	Latency  string  `json:"latency"`
	Replicas int     `json:"replicas"`
}

func (a *App) NodesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var nodes []NodeInfo
	uptimeDuration := time.Since(a.StartTime)
	uptimeStr := fmt.Sprintf("%dh %dm", int(uptimeDuration.Hours()), int(uptimeDuration.Minutes())%60)

	for i, srv := range a.Servers {
		if srv == nil {
			continue
		}

		var fileCount = 0
		var nodeBytes int64 = 0
		if srv.Metadata != nil {
			metaList := srv.Metadata.List()
			fileCount = len(metaList)
			for _, m := range metaList {
				nodeBytes += m.Size
			}
		}

		usedGB := float64(nodeBytes) / (1024 * 1024 * 1024)
		role := "Storage Peer"
		if i == 0 {
			role = "Bootstrap Leader"
		}

		nodeID := fmt.Sprintf("node-%s", srv.ListenAddr[1:])
		nodeName := fmt.Sprintf("Node %s (%s)", srv.ListenAddr[1:], role)

		nodes = append(nodes, NodeInfo{
			ID:       nodeID,
			Name:     nodeName,
			Addr:     srv.ListenAddr,
			Status:   "Online",
			Storage:  fmt.Sprintf("%.2f GB / 4.0 TB", usedGB),
			Used:     usedGB,
			Total:    4.0,
			Role:     role,
			Uptime:   uptimeStr,
			Latency:  "1ms",
			Replicas: fileCount,
		})
	}

	json.NewEncoder(w).Encode(nodes)
}

