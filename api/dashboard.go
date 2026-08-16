package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type DashboardResponse struct {
	TotalStorage         float64 `json:"totalStorage"`         // Total Storage in GB (400.0 GB)
	UsedStorage          float64 `json:"usedStorage"`          // Used Storage in GB
	UsedStorageBytes     int64   `json:"usedStorageBytes"`     // Exact Used Bytes
	UsedStorageFormatted string  `json:"usedStorageFormatted"` // Human readable string e.g. "44.5 KB"
	StorageUsedPercent   float64 `json:"storageUsedPercent"`   // Percentage of total capacity
	ActiveNodes          int     `json:"activeNodes"`          // Active Nodes count
	ReplicationHealth    float64 `json:"replicationHealth"`    // Replication percentage
	Availability         float64 `json:"availability"`         // System availability percentage
	GDriveConnected      bool    `json:"gdriveConnected"`      // Google Drive connected status
}

func (a *App) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	activeNodes := len(a.Servers)
	var totalBytes int64 = 0

	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			metaList := srv.Metadata.List()
			for _, m := range metaList {
				totalBytes += m.Size
			}
		}
	}

	totalCapacityGB := 400.0 // 400 GB Google Drive Cloud Storage Vault
	usedGB := float64(totalBytes) / (1024 * 1024 * 1024)
	usedPercent := (usedGB / totalCapacityGB) * 100.0
	if usedPercent < 0.01 && totalBytes > 0 {
		usedPercent = 0.01
	}

	formattedUsed := formatBytes(totalBytes)

	gdriveConnected := false
	if len(a.Servers) > 0 && a.Servers[0].GDrive != nil && a.Servers[0].GDrive.Enabled {
		gdriveConnected = true
	}

	response := DashboardResponse{
		TotalStorage:         totalCapacityGB,
		UsedStorage:          usedGB,
		UsedStorageBytes:     totalBytes,
		UsedStorageFormatted: formattedUsed,
		StorageUsedPercent:   usedPercent,
		ActiveNodes:          activeNodes,
		ReplicationHealth:    100.0,
		Availability:         100.0,
		GDriveConnected:      gdriveConnected,
	}

	json.NewEncoder(w).Encode(response)
}

func formatBytes(bytes int64) string {
	const (
		_  = iota
		KB = 1 << (10 * iota)
		MB
		GB
		TB
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
