package api

import (
	"encoding/json"
	"net/http"
)

type ClusterStatusResponse struct {
	ClusterID         string  `json:"clusterId"`
	Status            string  `json:"status"`
	TotalStorage      float64 `json:"totalStorage"`
	UsedStorage       float64 `json:"usedStorage"`
	FreeStorage       float64 `json:"freeStorage"`
	ActiveNodes       int     `json:"activeNodes"`
	TotalNodes        int     `json:"totalNodes"`
	ReplicationFactor int     `json:"replicationFactor"`
	HealthScore       float64 `json:"healthScore"`
	Uptime            string  `json:"uptime"`
}

func (a *App) ClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ClusterStatusResponse{
		ClusterID:         "nazeerdfs-prod-01",
		Status:            "Healthy",
		TotalStorage:      12.0,
		UsedStorage:       7.3,
		FreeStorage:       4.7,
		ActiveNodes:       3,
		TotalNodes:        3,
		ReplicationFactor: 3,
		HealthScore:       99.98,
		Uptime:            "45 days, 12 hours",
	})
}
