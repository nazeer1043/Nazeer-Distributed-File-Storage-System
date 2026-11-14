package api

import "net/http"

// RegisterRoutes registers all HTTP API routes onto the provided ServeMux using the App instance.
func RegisterRoutes(mux *http.ServeMux, a *App) {
	mux.HandleFunc("/api/login", a.LoginHandler)
	mux.HandleFunc("/api/register", a.RegisterHandler)
	mux.HandleFunc("/api/logout", a.LogoutHandler)
	mux.HandleFunc("/api/session", a.SessionHandler)
	mux.HandleFunc("/api/dashboard", a.RequireAuth(a.DashboardHandler))
	mux.HandleFunc("/api/files", a.RequireAuth(a.FilesHandler))
	mux.HandleFunc("/api/upload", a.RequireAuth(a.UploadHandler))
	mux.HandleFunc("/api/download", a.RequireAuth(a.DownloadHandler))
	mux.HandleFunc("/api/delete", a.RequireAuth(a.DeleteHandler))
	mux.HandleFunc("/api/nodes", a.RequireAuth(a.NodesHandler))
	mux.HandleFunc("/api/cluster", a.RequireAuth(a.ClusterHandler))
	mux.HandleFunc("/api/logs", a.RequireAuth(a.LogsHandler))
	mux.HandleFunc("/api/settings", a.RequireAuth(a.SettingsHandler))
}

