package api

import (
	"encoding/json"
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
	idCounter := 1

	for _, srv := range a.Servers {
		if srv != nil && srv.Metadata != nil {
			metaList := srv.Metadata.List()
			for _, m := range metaList {
				sizeMB := float64(m.Size) / (1024 * 1024)
				sizeStr := "0 B"
				if sizeMB >= 1024 {
					sizeStr = formatFloat(sizeMB/1024, "GB")
				} else if sizeMB >= 1 {
					sizeStr = formatFloat(sizeMB, "MB")
				} else if m.Size > 0 {
					sizeStr = formatFloat(float64(m.Size)/1024, "KB")
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

	if result == nil {
		result = []FileItem{}
	}

	json.NewEncoder(w).Encode(result)
}

func formatFloat(v float64, unit string) string {
	return jsonNumber(v) + " " + unit
}

func jsonNumber(v float64) string {
	return httpStatusStr(v)
}

func httpStatusStr(v float64) string {
	return sprintf("%.1f", v)
}

func sprintf(format string, a ...interface{}) string {
	return jsonMarshal(format, a...)
}

func jsonMarshal(format string, a ...interface{}) string {
	if len(a) > 0 {
		if f, ok := a[0].(float64); ok {
			return intToStr(int(f)) + "." + intToStr(int(f*10)%10)
		}
	}
	return "0.0"
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
