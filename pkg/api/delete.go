package api

import (
	"encoding/json"
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

	for _, srv := range a.Servers {
		if srv != nil {
			_ = srv.Storage.Delete(srv.ID, key)
			if srv.Metadata != nil {
				_ = srv.Metadata.Delete(key)
			}
		}
	}

	if a.DB != nil {
		_ = a.DB.DeleteFileRecord(key)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DeleteResponse{
		Success: true,
		Message: "File successfully purged from all cluster storage nodes.",
	})
}

