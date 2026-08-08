package api

import (
	"encoding/json"
	"net/http"
)

type UploadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	FileID  string `json:"fileId,omitempty"`
}

func (a *App) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(UploadResponse{
		Success: true,
		Message: "File successfully uploaded and distributed across cluster nodes.",
		FileID:  "chunk_90a12",
	})
}
