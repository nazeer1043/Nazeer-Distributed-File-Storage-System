package api

import (
	"encoding/json"
	"net/http"
)

type DashboardResponse struct {
	TotalStorage      float64 `json:"totalStorage"`
	UsedStorage       float64 `json:"usedStorage"`
	ActiveNodes       int     `json:"activeNodes"`
	ReplicationHealth float64 `json:"replicationHealth"`
	Availability      float64 `json:"availability"`
}

func (a *App) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	response := DashboardResponse{
		TotalStorage:      12,
		UsedStorage:       7.3,
		ActiveNodes:       3,
		ReplicationHealth: 100,
		Availability:      100,
	}

	json.NewEncoder(w).Encode(response)
}
