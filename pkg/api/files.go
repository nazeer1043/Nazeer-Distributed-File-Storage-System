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
	RefCount   int    `json:"refCount"`
}

func formatSize(bytes int64) string {
	sizeMB := float64(bytes) / (1024 * 1024)
	if sizeMB >= 1024 {
		return fmt.Sprintf("%.2f GB", sizeMB/1024)
	} else if sizeMB >= 1 {
		return fmt.Sprintf("%.1f MB", sizeMB)
	} else if bytes > 0 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return "0 B"
}

// FilesHandler lists stored files. MySQL serves as the primary source of truth,
// with local metadata.json retained as an optional development fallback.
func (a *App) FilesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	user, ok := a.CurrentUser(r)
	if !ok || user == nil || user.Username == "" {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	isAdmin := (user.Role == "admin" || user.Role == "Cluster Admin")

	var result []FileItem
	seenKeys := make(map[string]bool)
	idCounter := 1

	nodeAddr := ":3000"
	if len(a.Servers) > 0 {
		nodeAddr = a.Servers[0].ListenAddr
	}

	// 1. MySQL is the primary source of truth for metadata
	if a.DB != nil && a.DB.Active {
		if dbFiles, err := a.DB.GetFileRecords(); err == nil {
			for _, m := range dbFiles {
				if !isAdmin && m.Owner != user.Username {
					continue
				}
				if seenKeys[m.Key] {
					continue
				}
				seenKeys[m.Key] = true

				result = append(result, FileItem{
					ID:         idCounter,
					Name:       m.Filename,
					Size:       formatSize(m.Size),
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
					RefCount:   m.RefCount,
				})
				idCounter++
			}

			if result == nil {
				result = []FileItem{}
			}
			json.NewEncoder(w).Encode(result)
			return
		}
	}

	// 2. Development fallback: local metadata.json
	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			metaList := srv.Metadata.List()
			for _, m := range metaList {
				if !isAdmin && m.Owner != user.Username {
					continue
				}
				if seenKeys[m.Key] {
					continue
				}
				seenKeys[m.Key] = true

				result = append(result, FileItem{
					ID:         idCounter,
					Name:       m.Filename,
					Size:       formatSize(m.Size),
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
					RefCount:   m.RefCount,
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
