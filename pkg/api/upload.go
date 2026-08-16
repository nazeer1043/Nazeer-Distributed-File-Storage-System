package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type UploadResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	FileID   string `json:"fileId,omitempty"`
	Filename string `json:"filename,omitempty"`
}

func (a *App) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Parse up to 1GB multipart form
	if err := r.ParseMultipartForm(1024 * 1024 * 1024); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "Failed to parse multipart form payload",
		})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "No file field found in form request",
		})
		return
	}
	defer file.Close()

	if len(a.Servers) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "No storage servers running in cluster",
		})
		return
	}

	owner := r.FormValue("owner")
	if owner == "" {
		owner = "System Admin"
	}

	// Generate deterministic storage key from filename hash
	keyHash := sha256.Sum256([]byte(header.Filename))
	key := hex.EncodeToString(keyHash[:16])

	meta, err := a.Servers[0].StoreWithMeta(key, header.Filename, owner, header.Header.Get("Content-Type"), file)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "Storage error: " + err.Error(),
		})
		return
	}

	if a.DB != nil {
		_ = a.DB.SaveFileRecord(meta)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(UploadResponse{
		Success:  true,
		Message:  "File successfully stored in CAS storage and distributed across peer nodes.",
		FileID:   meta.Key,
		Filename: meta.Filename,
	})
}

