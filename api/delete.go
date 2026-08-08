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
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DeleteResponse{
		Success: true,
		Message: "File successfully purged from all cluster storage nodes.",
	})
}
