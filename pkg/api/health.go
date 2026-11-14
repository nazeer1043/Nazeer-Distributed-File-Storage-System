package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HealthzHandler responds to liveness probes.
func (a *App) HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "alive",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// ReadyzHandler responds to readiness probes, validating backend storage nodes and database connectivity.
func (a *App) ReadyzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	healthyNodes := a.GetHealthyServers()
	dbStatus := "disabled"
	isReady := true
	var issues []string

	// Check storage nodes
	if len(healthyNodes) == 0 {
		isReady = false
		issues = append(issues, "no healthy storage nodes available in cluster")
	}

	// Check database if configured
	if a.DB != nil && a.DB.Active {
		dbStatus = "connected"
		if err := a.DB.Ping(); err != nil {
			isReady = false
			dbStatus = "unreachable"
			issues = append(issues, "database ping failed: "+err.Error())
		}
	}

	statusCode := http.StatusOK
	statusStr := "ready"
	if !isReady {
		statusCode = http.StatusServiceUnavailable
		statusStr = "not_ready"
	}

	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       statusStr,
		"healthyNodes": len(healthyNodes),
		"totalNodes":   len(a.Servers),
		"database":     dbStatus,
		"issues":       issues,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	})
}

// MetricsHandler serves Prometheus metrics in the standard text exposition format.
func (a *App) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	uptime := time.Since(a.StartTime).Seconds()
	healthyNodes := a.GetHealthyServers()
	activeCount := len(healthyNodes)
	totalCount := len(a.Servers)

	var totalFiles int = 0
	var totalBytes int64 = 0

	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			list := srv.Metadata.List()
			totalFiles += len(list)
			for _, m := range list {
				totalBytes += m.Size
			}
		}
	}

	gdriveConnected := 0
	for _, srv := range a.Servers {
		if srv != nil && srv.GDrive != nil && srv.GDrive.Enabled {
			gdriveConnected = 1
			break
		}
	}

	fmt.Fprintf(w, "# HELP nazeerdfs_uptime_seconds Total runtime of the NazeerDFS service\n")
	fmt.Fprintf(w, "# TYPE nazeerdfs_uptime_seconds gauge\n")
	fmt.Fprintf(w, "nazeerdfs_uptime_seconds %.2f\n\n", uptime)

	fmt.Fprintf(w, "# HELP nazeerdfs_active_nodes Number of responsive storage nodes\n")
	fmt.Fprintf(w, "# TYPE nazeerdfs_active_nodes gauge\n")
	fmt.Fprintf(w, "nazeerdfs_active_nodes %d\n\n", activeCount)

	fmt.Fprintf(w, "# HELP nazeerdfs_total_nodes Total configured cluster nodes\n")
	fmt.Fprintf(w, "# TYPE nazeerdfs_total_nodes gauge\n")
	fmt.Fprintf(w, "nazeerdfs_total_nodes %d\n\n", totalCount)

	fmt.Fprintf(w, "# HELP nazeerdfs_stored_files Number of stored file objects\n")
	fmt.Fprintf(w, "# TYPE nazeerdfs_stored_files gauge\n")
	fmt.Fprintf(w, "nazeerdfs_stored_files %d\n\n", totalFiles)

	fmt.Fprintf(w, "# HELP nazeerdfs_stored_bytes Total volume of stored file objects in bytes\n")
	fmt.Fprintf(w, "# TYPE nazeerdfs_stored_bytes gauge\n")
	fmt.Fprintf(w, "nazeerdfs_stored_bytes %d\n\n", totalBytes)

	fmt.Fprintf(w, "# HELP nazeerdfs_gdrive_connected Status of Google Drive Cloud Backup Tier (1=active, 0=inactive)\n")
	fmt.Fprintf(w, "# TYPE nazeerdfs_gdrive_connected gauge\n")
	fmt.Fprintf(w, "nazeerdfs_gdrive_connected %d\n", gdriveConnected)
}
