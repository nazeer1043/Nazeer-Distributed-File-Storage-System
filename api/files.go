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

	files := []FileItem{
		{ID: 1, Name: "annual-report-2026.pdf", Size: "24.8 MB", RawSize: 25999000, Category: "Documents", Owner: "Arjun Nazeer", Node: "Node 3000", NodeColor: "text-brand-500 bg-brand-500/10", Replicas: "3/3", Status: "Healthy", Modified: "2026-07-28 14:20", Icon: "file-text", IconColor: "text-red-500", Checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", Encryption: "AES-256-GCM", Downloads: "1,420", Location: "node-3000:/data/chunks/ch_8f9a2"},
		{ID: 2, Name: "client-backup.tar.gz", Size: "14.2 GB", RawSize: 15247000000, Category: "Archives", Owner: "DevOps Team", Node: "Node 4000", NodeColor: "text-amber-500 bg-amber-500/10", Replicas: "3/3", Status: "Healthy", Modified: "2026-07-28 11:45", Icon: "archive", IconColor: "text-amber-500", Checksum: "8f4a2190b2e3158c67a3f1295914197365287f1a", Encryption: "AES-256-GCM", Downloads: "384", Location: "node-4000:/data/backups/bk_491a"},
		{ID: 3, Name: "holiday-photos.zip", Size: "1.2 GB", RawSize: 1288000000, Category: "Archives", Owner: "Sarah Connor", Node: "Node 5000", NodeColor: "text-cyan-500 bg-cyan-500/10", Replicas: "2/3", Status: "Replicating", Modified: "2026-07-27 18:30", Icon: "file-archive", IconColor: "text-brand-500", Checksum: "c4a91b290184a7e930129fbc8129731", Encryption: "AES-256-GCM", Downloads: "52", Location: "node-5000:/data/media/ph_102"},
		{ID: 4, Name: "financial-data.xlsx", Size: "8.4 MB", RawSize: 8808000, Category: "Documents", Owner: "Finance Lead", Node: "Node 3000", NodeColor: "text-brand-500 bg-brand-500/10", Replicas: "3/3", Status: "Healthy", Modified: "2026-07-27 09:15", Icon: "file-spreadsheet", IconColor: "text-emerald-500", Checksum: "7190abc12019485bc920412895710", Encryption: "AES-256-GCM", Downloads: "890", Location: "node-3000:/data/finance/fn_2026"},
		{ID: 5, Name: "cluster-log-28-july.txt", Size: "512 KB", RawSize: 524288, Category: "Others", Owner: "System Automated", Node: "Node 4000", NodeColor: "text-amber-500 bg-amber-500/10", Replicas: "3/3", Status: "Syncing", Modified: "2026-07-28 20:05", Icon: "file-code", IconColor: "text-blue-500", Checksum: "3f90129a1b2c4d5e6f7a8b9c0d1e2f3a", Encryption: "AES-256-GCM", Downloads: "2,150", Location: "node-4000:/var/log/cluster_28Jul"},
		{ID: 6, Name: "presentation.pptx", Size: "45.1 MB", RawSize: 47290000, Category: "Documents", Owner: "Product Manager", Node: "Node 3000", NodeColor: "text-brand-500 bg-brand-500/10", Replicas: "3/3", Status: "Healthy", Modified: "2026-07-26 16:40", Icon: "presentation", IconColor: "text-orange-500", Checksum: "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d", Encryption: "AES-256-GCM", Downloads: "67", Location: "node-3000:/data/decks/deck_v4"},
		{ID: 7, Name: "promo-video-4k.mp4", Size: "3.8 GB", RawSize: 4080000000, Category: "Videos", Owner: "Marketing Team", Node: "Node 5000", NodeColor: "text-cyan-500 bg-cyan-500/10", Replicas: "3/3", Status: "Healthy", Modified: "2026-07-25 12:10", Icon: "video", IconColor: "text-purple-500", Checksum: "11223344556677889900aabbccddeeff", Encryption: "AES-256-GCM", Downloads: "310", Location: "node-5000:/data/videos/promo_4k"},
		{ID: 8, Name: "kernel-build-v5.iso", Size: "4.7 GB", RawSize: 5046000000, Category: "Others", Owner: "Linux Kernel Team", Node: "Node 4000", NodeColor: "text-amber-500 bg-amber-500/10", Replicas: "1/3", Status: "Warning", Modified: "2026-07-24 17:00", Icon: "disc", IconColor: "text-slate-400", Checksum: "aabbccdd112233445566778899001122", Encryption: "AES-256-GCM", Downloads: "145", Location: "node-4000:/data/iso/kernel_v5"},
		{ID: 9, Name: "customer-db-dump.sql", Size: "640 MB", RawSize: 671088640, Category: "Documents", Owner: "DB Admin", Node: "Node 3000", NodeColor: "text-brand-500 bg-brand-500/10", Replicas: "3/3", Status: "Healthy", Modified: "2026-07-28 08:30", Icon: "database", IconColor: "text-indigo-500", Checksum: "ffeeddccbbaa99887766554433221100", Encryption: "AES-256-GCM", Downloads: "93", Location: "node-3000:/data/db/prod_dump_jul"},
		{ID: 10, Name: "hero-banner-hd.png", Size: "18.4 MB", RawSize: 19293798, Category: "Images", Owner: "Design Lead", Node: "Node 5000", NodeColor: "text-cyan-500 bg-cyan-500/10", Replicas: "3/3", Status: "Healthy", Modified: "2026-07-28 13:00", Icon: "image", IconColor: "text-pink-500", Checksum: "1234567890abcdef1234567890abcdef", Encryption: "AES-256-GCM", Downloads: "540", Location: "node-5000:/data/design/banner_hd"},
	}

	json.NewEncoder(w).Encode(files)
}
