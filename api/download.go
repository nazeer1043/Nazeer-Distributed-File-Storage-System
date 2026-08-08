package api

import (
	"encoding/json"
	"net/http"
)

type DownloadResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	FileName string `json:"filename"`
}

func (a *App) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileName := r.URL.Query().Get("file")
	if fileName == "" {
		fileName = "downloaded-file.dat"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(DownloadResponse{
		Success:  true,
		Message:  "Download initiated successfully.",
		FileName: fileName,
	})
}
