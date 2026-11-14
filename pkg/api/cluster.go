package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type ClusterStatusResponse struct {
	ClusterID         string  `json:"clusterId"`
	Status            string  `json:"status"`
	TotalStorage      float64 `json:"totalStorage"`      // Capacity in GB (400.0)
	UsedStorage       float64 `json:"usedStorage"`       // Used storage in GB
	FreeStorage       float64 `json:"freeStorage"`       // Free storage in GB
	ActiveNodes       int     `json:"activeNodes"`
	TotalNodes        int     `json:"totalNodes"`
	ReplicationFactor int     `json:"replicationFactor"`
	HealthScore       float64 `json:"healthScore"`
	Uptime            string  `json:"uptime"`
	GDriveEnabled     bool    `json:"gdriveEnabled"`
}

func (a *App) ClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	healthyServers := a.GetHealthyServers()
	activeCount := len(healthyServers)
	totalCount := len(a.Servers)
	var totalClusterBytes int64 = 0

	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			metaList := srv.Metadata.List()
			for _, m := range metaList {
				totalClusterBytes += m.Size
			}
		}
	}

	totalCapacityGB := 400.0 // 400 GB Google Drive Cloud Storage
	usedGB := float64(totalClusterBytes) / (1024 * 1024 * 1024)
	freeGB := totalCapacityGB - usedGB

	gdriveActive := false
	for _, srv := range a.Servers {
		if srv != nil && srv.GDrive != nil && srv.GDrive.Enabled {
			gdriveActive = true
			break
		}
	}

	uptimeDuration := time.Since(a.StartTime)
	uptimeStr := fmt.Sprintf("%d hours, %d mins", int(uptimeDuration.Hours()), int(uptimeDuration.Minutes())%60)

	healthScore := 0.0
	if totalCount > 0 {
		healthScore = (float64(activeCount) / float64(totalCount)) * 100.0
	}

	json.NewEncoder(w).Encode(ClusterStatusResponse{
		ClusterID:         "nazeerdfs-cluster-01",
		Status:            "Healthy",
		TotalStorage:      totalCapacityGB,
		UsedStorage:       usedGB,
		FreeStorage:       freeGB,
		ActiveNodes:       activeCount,
		TotalNodes:        totalCount,
		ReplicationFactor: 2,
		HealthScore:       healthScore,
		Uptime:            uptimeStr,
		GDriveEnabled:     gdriveActive,
	})
}
