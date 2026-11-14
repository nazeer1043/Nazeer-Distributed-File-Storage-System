package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

	// Compute Content-Addressable Storage (CAS) key from filename, owner, and payload
	var reader io.Reader = file
	hasher := sha256.New()
	hasher.Write([]byte(header.Filename + ":" + owner + ":"))

	if seeker, ok := file.(io.Seeker); ok {
		if _, err := io.Copy(hasher, file); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(UploadResponse{
				Success: false,
				Message: "Failed to read upload payload: " + err.Error(),
			})
			return
		}
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(UploadResponse{
				Success: false,
				Message: "Failed to rewind payload stream: " + err.Error(),
			})
			return
		}
	} else {
		data, err := io.ReadAll(file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(UploadResponse{
				Success: false,
				Message: "Failed to read upload payload: " + err.Error(),
			})
			return
		}
		hasher.Write(data)
		reader = bytes.NewReader(data)
	}

	key := hex.EncodeToString(hasher.Sum(nil)[:16])

	meta, err := a.Servers[0].StoreWithMeta(key, header.Filename, owner, header.Header.Get("Content-Type"), reader)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "Storage error: " + err.Error(),
		})
		return
	}

	LogEvent("INFO", fmt.Sprintf("File '%s' stored in CAS with key %s and replicated across cluster", meta.Filename, meta.Key))

	if a.DB != nil {
		_ = a.DB.SaveFileRecord(meta)
		a.DB.InsertAuditLog("INFO", a.Servers[0].ListenAddr, fmt.Sprintf("Uploaded file '%s' (%d bytes, key: %s)", meta.Filename, meta.Size, meta.Key))
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(UploadResponse{
		Success:  true,
		Message:  "File successfully stored in CAS storage and distributed across peer nodes.",
		FileID:   meta.Key,
		Filename: meta.Filename,
	})
}

