package api

import "net/http"

// RegisterRoutes registers all HTTP API routes onto the provided ServeMux using the App instance.
func RegisterRoutes(mux *http.ServeMux, a *App) {
	mux.HandleFunc("/api/login", LoginHandler)
	mux.HandleFunc("/api/logout", LogoutHandler)
	mux.HandleFunc("/api/session", SessionHandler)
	mux.HandleFunc("/api/dashboard", RequireAuth(a.DashboardHandler))
	mux.HandleFunc("/api/files", RequireAuth(a.FilesHandler))
	mux.HandleFunc("/api/upload", RequireAuth(a.UploadHandler))
	mux.HandleFunc("/api/download", RequireAuth(a.DownloadHandler))
	mux.HandleFunc("/api/delete", RequireAuth(a.DeleteHandler))
	mux.HandleFunc("/api/nodes", RequireAuth(a.NodesHandler))
	mux.HandleFunc("/api/cluster", RequireAuth(a.ClusterHandler))
	mux.HandleFunc("/api/logs", RequireAuth(a.LogsHandler))
	mux.HandleFunc("/api/settings", RequireAuth(a.SettingsHandler))
}

