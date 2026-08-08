package api

import (
	"encoding/json"
	"net/http"
)

type SettingsData struct {
	ClusterName       string `json:"clusterName"`
	ReplicationFactor int    `json:"replicationFactor"`
	AutoBalance       bool   `json:"autoBalance"`
	EncryptionEnabled bool   `json:"encryptionEnabled"`
	CompressionLevel  string `json:"compressionLevel"`
	LogVerbosity      string `json:"logVerbosity"`
}

var currentSettings = SettingsData{
	ClusterName:       "NazeerDFS Enterprise",
	ReplicationFactor: 3,
	AutoBalance:       true,
	EncryptionEnabled: true,
	CompressionLevel:  "GZIP-High",
	LogVerbosity:      "INFO",
}

func (a *App) SettingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		json.NewEncoder(w).Encode(currentSettings)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var updated SettingsData
		if err := json.NewDecoder(r.Body).Decode(&updated); err == nil {
			currentSettings = updated
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"message":  "Cluster configuration saved successfully.",
			"settings": currentSettings,
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
