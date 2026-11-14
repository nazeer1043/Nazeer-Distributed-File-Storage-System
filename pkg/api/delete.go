package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type DeleteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (a *App) DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.URL.Query().Get("file")
	}

	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(DeleteResponse{
			Success: false,
			Message: "Missing file key parameter",
		})
		return
	}

	// Resolve actual target storage key if filename was provided
	targetKey := key
	targetFilename := key

	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			if meta, ok := srv.Metadata.Get(key); ok {
				targetKey = meta.Key
				targetFilename = meta.Filename
				break
			}
			for _, m := range srv.Metadata.List() {
				if m.Filename == key || m.Key == key {
					targetKey = m.Key
					targetFilename = m.Filename
					break
				}
			}
		}
	}

	// Delete locally and broadcast P2P deletion across all nodes and GDrive
	for _, srv := range a.Servers {
		if srv != nil {
			_ = srv.Delete(targetKey)
		}
	}

	LogEvent("WARN", fmt.Sprintf("File '%s' (key: %s) permanently deleted from cluster", targetFilename, targetKey))

	if a.DB != nil {
		_ = a.DB.DeleteFileRecord(targetKey)
		a.DB.InsertAuditLog("WARN", a.Servers[0].ListenAddr, fmt.Sprintf("File '%s' (key: %s) purged from cluster", targetFilename, targetKey))
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DeleteResponse{
		Success: true,
		Message: "File successfully purged from all cluster storage nodes.",
	})
}


