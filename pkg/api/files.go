package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type FileItem struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Size       string `json:"size"`
	RawSize    int64  `json:"rawSize"`
	Category   string `json:"category"`
	Owner      string `json:"owner"`
	Node       string `json:"node"`
	NodeColor  string `json:"nodeColor"`
	Replicas   string `json:"replicas"`
	Status     string `json:"status"`
	Modified   string `json:"modified"`
	Icon       string `json:"icon"`
	IconColor  string `json:"iconColor"`
	Checksum   string `json:"checksum"`
	Encryption string `json:"encryption"`
	Downloads  string `json:"downloads"`
	Location   string `json:"location"`
}

func (a *App) FilesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var result []FileItem
	seenKeys := make(map[string]bool)
	idCounter := 1

	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			metaList := srv.Metadata.List()
			for _, m := range metaList {
				if seenKeys[m.Key] {
					continue
				}
				seenKeys[m.Key] = true

				sizeMB := float64(m.Size) / (1024 * 1024)
				sizeStr := "0 B"
				if sizeMB >= 1024 {
					sizeStr = fmt.Sprintf("%.2f GB", sizeMB/1024)
				} else if sizeMB >= 1 {
					sizeStr = fmt.Sprintf("%.1f MB", sizeMB)
				} else if m.Size > 0 {
					sizeStr = fmt.Sprintf("%.1f KB", float64(m.Size)/1024)
				}

				result = append(result, FileItem{
					ID:         idCounter,
					Name:       m.Filename,
					Size:       sizeStr,
					RawSize:    m.Size,
					Category:   "Documents",
					Owner:      m.Owner,
					Node:       srv.ListenAddr,
					NodeColor:  "text-brand-500 bg-brand-500/10",
					Replicas:   "3/3",
					Status:     "Healthy",
					Modified:   m.UploadTime.Format("2006-01-02 15:04"),
					Icon:       "file-text",
					IconColor:  "text-brand-500",
					Checksum:   m.Checksum,
					Encryption: "AES-256-GCM",
					Downloads:  "0",
					Location:   m.Key,
				})
				idCounter++
			}
		}
	}

	// If local metadata is empty, fallback to querying MySQL records
	if len(result) == 0 && a.DB != nil && a.DB.Active {
		if dbFiles, err := a.DB.GetFileRecords(); err == nil {
			for _, m := range dbFiles {
				if seenKeys[m.Key] {
					continue
				}
				seenKeys[m.Key] = true

				sizeMB := float64(m.Size) / (1024 * 1024)
				sizeStr := "0 B"
				if sizeMB >= 1024 {
					sizeStr = fmt.Sprintf("%.2f GB", sizeMB/1024)
				} else if sizeMB >= 1 {
					sizeStr = fmt.Sprintf("%.1f MB", sizeMB)
				} else if m.Size > 0 {
					sizeStr = fmt.Sprintf("%.1f KB", float64(m.Size)/1024)
				}

				nodeAddr := ":3000"
				if len(a.Servers) > 0 {
					nodeAddr = a.Servers[0].ListenAddr
				}

				result = append(result, FileItem{
					ID:         idCounter,
					Name:       m.Filename,
					Size:       sizeStr,
					RawSize:    m.Size,
					Category:   "Documents",
					Owner:      m.Owner,
					Node:       nodeAddr,
					NodeColor:  "text-brand-500 bg-brand-500/10",
					Replicas:   "3/3",
					Status:     "Healthy",
					Modified:   m.UploadTime.Format("2006-01-02 15:04"),
					Icon:       "file-text",
					IconColor:  "text-brand-500",
					Checksum:   m.Checksum,
					Encryption: "AES-256-GCM",
					Downloads:  "0",
					Location:   m.Key,
				})
				idCounter++
			}
		}
	}

	if result == nil {
		result = []FileItem{}
	}

	json.NewEncoder(w).Encode(result)
}

