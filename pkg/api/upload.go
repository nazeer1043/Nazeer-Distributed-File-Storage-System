package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

type UploadResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	FileID   string `json:"fileId,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// UploadHandler streams multipart uploads into a temporary file while hashing (no whole-file RAM buffers),
// derives a namespaced CAS storage key, and deduplicates identical content per owner.
func (a *App) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	user, ok := a.CurrentUser(r)
	if !ok || user == nil || user.Username == "" {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "Authentication required to upload files",
		})
		return
	}
	owner := user.Username

	if len(a.Servers) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "No storage servers running in cluster",
		})
		return
	}

	// 1. Enforce configurable maximum upload size with http.MaxBytesReader
	maxUploadSize := int64(1024 * 1024 * 1024) // 1GB default
	if a.Cfg != nil && a.Cfg.MaxUploadSize > 0 {
		maxUploadSize = a.Cfg.MaxUploadSize
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// 2. Stream multipart form using MultipartReader (no ParseMultipartForm whole-file buffering)
	mr, err := r.MultipartReader()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "Failed to read multipart stream: " + err.Error(),
		})
		return
	}

	var (
		tmpFile     *os.File
		filename    string
		contentType string
		fileSize    int64
		contentSHA  string
	)

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(UploadResponse{
				Success: false,
				Message: "Failed to read multipart part: " + err.Error(),
			})
			return
		}

		if part.FormName() == "file" {
			filename = part.FileName()
			contentType = part.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "application/octet-stream"
			}

			// Stream directly to temp file while computing full SHA-256
			tmpFile, err = os.CreateTemp("", "nazeerdfs_upload_*")
			if err != nil {
				part.Close()
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(UploadResponse{
					Success: false,
					Message: "Failed to create temp upload file: " + err.Error(),
				})
				return
			}

			hasher := sha256.New()
			written, copyErr := io.Copy(io.MultiWriter(tmpFile, hasher), part)
			part.Close()
			if copyErr != nil {
				_ = tmpFile.Close()
				_ = os.Remove(tmpFile.Name())
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(UploadResponse{
					Success: false,
					Message: "Upload stream read error: " + copyErr.Error(),
				})
				return
			}

			fileSize = written
			contentSHA = hex.EncodeToString(hasher.Sum(nil))
			break
		}
		part.Close()
	}

	if tmpFile == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "No file field found in multipart payload",
		})
		return
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	// 3. Make key genuinely content-derived: full SHA-256 of plaintext content, namespaced per owner.
	keyHasher := sha256.New()
	keyHasher.Write([]byte(owner + ":" + contentSHA))
	casKey := hex.EncodeToString(keyHasher.Sum(nil))

	targetServer, err := a.SelectServer(casKey)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "No healthy storage nodes available in cluster: " + err.Error(),
		})
		return
	}

	// 4. Deduplication: check if identical content already exists for this owner
	var existingMeta *fileserver.FileMeta
	if a.DB != nil && a.DB.Active {
		if m, err := a.DB.GetFileRecordByKey(casKey); err == nil && m != nil {
			existingMeta = m
		}
	}
	if existingMeta == nil && targetServer.Metadata != nil {
		if m, ok := targetServer.Metadata.Get(casKey); ok {
			existingMeta = m
		}
	}

	if existingMeta != nil {
		// Content deduplicated! Increment reference count in MySQL and metadata store.
		existingMeta.Filename = filename // update metadata record with requested filename
		if a.DB != nil && a.DB.Active {
			_ = a.DB.SaveFileRecord(existingMeta)
			a.DB.InsertAuditLog("INFO", targetServer.ListenAddr, fmt.Sprintf("Deduplicated file '%s' (reused CAS key %s)", filename, casKey))
		}
		if targetServer.Metadata != nil {
			_ = targetServer.Metadata.Put(existingMeta)
		}

		LogEvent("INFO", fmt.Sprintf("File '%s' deduplicated via CAS key %s (reference count incremented)", filename, casKey))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(UploadResponse{
			Success:  true,
			Message:  "File content identical to existing upload; deduplicated with reference count increment.",
			FileID:   casKey,
			Filename: filename,
		})
		return
	}

	// 5. Rewind temp file and stream into encrypted storage
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "Failed to rewind temp file stream: " + err.Error(),
		})
		return
	}

	meta, err := targetServer.StoreWithMeta(casKey, filename, owner, contentType, contentSHA, fileSize, tmpFile)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(UploadResponse{
			Success: false,
			Message: "Storage error: " + err.Error(),
		})
		return
	}

	LogEvent("INFO", fmt.Sprintf("File '%s' stored in CAS with key %s and replicated across cluster", meta.Filename, meta.Key))

	if a.DB != nil && a.DB.Active {
		_ = a.DB.SaveFileRecord(meta)
		a.DB.InsertAuditLog("INFO", targetServer.ListenAddr, fmt.Sprintf("Uploaded file '%s' (%d bytes, key: %s)", meta.Filename, meta.Size, meta.Key))
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(UploadResponse{
		Success:  true,
		Message:  "File successfully stored in CAS storage and distributed across peer nodes.",
		FileID:   meta.Key,
		Filename: meta.Filename,
	})
}
