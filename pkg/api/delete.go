package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

type DeleteResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DeleteHandler purges a file by key with strict ownership authorization.
func (a *App) DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	user, ok := a.CurrentUser(r)
	if !ok || user == nil || user.Username == "" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(DeleteResponse{
			Success: false,
			Message: "Authentication required",
		})
		return
	}
	isAdmin := (user.Role == "admin" || user.Role == "Cluster Admin")

	key := r.URL.Query().Get("key")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(DeleteResponse{
			Success: false,
			Message: "Missing file key parameter",
		})
		return
	}

	// Look up file metadata strictly by key (never by filename across all users)
	var meta *fileserver.FileMeta
	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			if m, exists := srv.Metadata.Get(key); exists {
				meta = m
				break
			}
		}
	}

	if meta == nil && a.DB != nil && a.DB.Active {
		if m, err := a.DB.GetFileRecordByKey(key); err == nil && m != nil {
			meta = m
		}
	}

	if meta == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(DeleteResponse{
			Success: false,
			Message: "File not found",
		})
		return
	}

	// Enforce ownership: only owner or admin can delete
	if !isAdmin && meta.Owner != user.Username {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(DeleteResponse{
			Success: false,
			Message: "Forbidden: you do not have permission to delete this file",
		})
		return
	}

	targetFilename := meta.Filename

	// Delete locally and broadcast P2P deletion across all nodes
	for _, srv := range a.Servers {
		if srv != nil {
			_ = srv.Delete(key)
		}
	}

	LogEvent("WARN", fmt.Sprintf("File '%s' (key: %s) deleted from cluster by %s", targetFilename, key, user.Username))

	if a.DB != nil && a.DB.Active {
		_ = a.DB.DeleteFileRecord(key)
		listenAddr := ":3000"
		if len(a.Servers) > 0 {
			listenAddr = a.Servers[0].ListenAddr
		}
		a.DB.InsertAuditLog("WARN", listenAddr, fmt.Sprintf("File '%s' (key: %s) deleted by %s", targetFilename, key, user.Username))
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DeleteResponse{
		Success: true,
		Message: "File successfully purged from storage.",
	})
}
