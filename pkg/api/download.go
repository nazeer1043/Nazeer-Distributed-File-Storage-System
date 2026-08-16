package api

import (
	"fmt"
	"io"
	"net/http"
)

func (a *App) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.URL.Query().Get("file")
	}

	if key == "" {
		http.Error(w, "Missing file or key query parameter", http.StatusBadRequest)
		return
	}

	if len(a.Servers) == 0 {
		http.Error(w, "No storage servers available", http.StatusInternalServerError)
		return
	}

	var filename = key
	var contentType = "application/octet-stream"

	if a.Servers[0].Metadata != nil {
		if meta, ok := a.Servers[0].Metadata.Get(key); ok {
			filename = meta.Filename
			if meta.ContentType != "" {
				contentType = meta.ContentType
			}
		}
	}

	reader, err := a.Servers[0].Get(key)
	if err != nil {
		http.Error(w, fmt.Sprintf("File not found or storage error: %v", err), http.StatusNotFound)
		return
	}

	if rc, ok := reader.(io.ReadCloser); ok {
		defer rc.Close()
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	io.Copy(w, reader)
}

