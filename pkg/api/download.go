package api

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

// sanitizeFilename strips directory traversal characters, control runes, and quotes from a filename.
func sanitizeFilename(name string) string {
	clean := filepath.Base(name)
	clean = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '/' || r == '\\' || r == '"' {
			return '_'
		}
		return r
	}, clean)
	clean = strings.TrimSpace(clean)
	if clean == "" || clean == "." || clean == ".." {
		clean = "download.bin"
	}
	return clean
}

// DownloadHandler serves stored files by key with strict ownership authorization,
// secure Content-Disposition formatting, and nosniff protection.
func (a *App) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "Missing key parameter", http.StatusBadRequest)
		return
	}

	if len(a.Servers) == 0 {
		http.Error(w, "No storage servers available", http.StatusInternalServerError)
		return
	}

	user, ok := a.CurrentUser(r)
	if !ok || user == nil || user.Username == "" {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	isAdmin := (user.Role == "admin" || user.Role == "Cluster Admin")

	// Look up file metadata strictly by key (never by filename search across all users)
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
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Enforce ownership: only owner or admin can download
	if !isAdmin && meta.Owner != user.Username {
		http.Error(w, "Forbidden: you do not have permission to access this file", http.StatusForbidden)
		return
	}

	reader, err := a.Servers[0].Get(key)
	if err != nil {
		http.Error(w, fmt.Sprintf("File not found or storage error: %v", err), http.StatusNotFound)
		return
	}

	if rc, ok := reader.(io.ReadCloser); ok {
		defer rc.Close()
	}

	filename := sanitizeFilename(meta.Filename)
	contentType := meta.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	disposition := mime.FormatMediaType("attachment", map[string]string{
		"filename": filename,
	})

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, reader)
}
