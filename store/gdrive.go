package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const DefaultVaultFolderName = "NazeerDFS_Vault"

// GDriveStore handles Google Drive cloud backup operations.
type GDriveStore struct {
	mu            sync.RWMutex
	service       *drive.Service
	folderID      string
	Enabled       bool
	CredsFilePath string
}

// NewGDriveStore initializes a Google Drive storage engine.
// If credentialsPath does not exist, it gracefully runs in disabled state with log notice.
func NewGDriveStore(credentialsPath string, vaultFolderName string) *GDriveStore {
	gs := &GDriveStore{
		CredsFilePath: credentialsPath,
		Enabled:       false,
	}

	if vaultFolderName == "" {
		vaultFolderName = DefaultVaultFolderName
	}

	credsBytes, err := os.ReadFile(credentialsPath)
	if err != nil {
		if envCreds := os.Getenv("CREDENTIALS_JSON"); envCreds != "" {
			credsBytes = []byte(envCreds)
		} else {
			log.Printf("[GDrive] Notice: '%s' not found in project root. Cloud backup tier running in disabled mode until credentials.json is provided.\n", credentialsPath)
			return gs
		}
	}

	ctx := context.Background()
	var srv *drive.Service

	if strings.Contains(string(credsBytes), `"service_account"`) {
		srv, err = drive.NewService(ctx, option.WithCredentialsFile(credentialsPath), option.WithScopes(drive.DriveScope))
		if err != nil {
			log.Printf("[GDrive] Warning: Failed to initialize Google Drive service account: %v\n", err)
			return gs
		}
	} else {
		httpClient, oauthErr := getOAuthClient(ctx, credsBytes)
		if oauthErr != nil {
			log.Printf("[GDrive] Warning: Failed to authorize personal Google Drive account: %v\n", oauthErr)
			return gs
		}
		srv, err = drive.NewService(ctx, option.WithHTTPClient(httpClient))
		if err != nil {
			log.Printf("[GDrive] Warning: Failed to create Drive service from OAuth client: %v\n", err)
			return gs
		}
	}

	folderID, err := getOrCreateFolder(srv, vaultFolderName)
	if err != nil {
		log.Printf("[GDrive] Warning: Failed to access '%s' folder on Google Drive: %v\n", vaultFolderName, err)
		return gs
	}

	gs.service = srv
	gs.folderID = folderID
	gs.Enabled = true

	log.Printf("[GDrive] Successfully initialized Google Drive 400GB Cloud Backup Tier (Folder ID: %s)\n", folderID)
	return gs
}

func getOAuthClient(ctx context.Context, credsBytes []byte) (*http.Client, error) {
	config, err := google.ConfigFromJSON(credsBytes, drive.DriveScope)
	if err != nil {
		return nil, err
	}

	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		config.RedirectURL = "http://localhost:8089/oauth/callback"
		codeChan := make(chan string)

		mux := http.NewServeMux()
		server := &http.Server{
			Addr:    ":8089",
			Handler: mux,
		}

		mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code != "" {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintln(w, "<html><body style='font-family:sans-serif;text-align:center;padding-top:50px;'><h1 style='color:#10b981;'>Google Drive Authorized!</h1><p>NazeerDFS has been granted access to your 400GB Google Drive.</p><p>You can close this window now.</p></body></html>")
				codeChan <- code
			}
		})

		go func() {
			_ = server.ListenAndServe()
		}()

		authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
		log.Printf("\n=====================================================================\n[GDrive] Action Required: Click URL to authorize your 400GB Google Drive:\n\n%s\n=====================================================================\n", authURL)

		authCode := <-codeChan
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)

		tok, err = config.Exchange(ctx, authCode)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve token from web: %w", err)
		}
		saveToken(tokFile, tok)
	}

	return config.Client(ctx, tok), nil
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	if envTok := os.Getenv("TOKEN_JSON"); envTok != "" {
		tok := &oauth2.Token{}
		if err := json.Unmarshal([]byte(envTok), tok); err == nil {
			return tok, nil
		}
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func saveToken(path string, token *oauth2.Token) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Printf("[GDrive] Unable to cache oauth token: %v\n", err)
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(token)
}

// UploadFile uploads an encrypted payload to the NazeerDFS_Vault folder on Google Drive.
func (gs *GDriveStore) UploadFile(key string, filename string, r io.Reader) (string, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if !gs.Enabled || gs.service == nil {
		return "", nil
	}

	driveFile := &drive.File{
		Name:    key + "_" + filename,
		Parents: []string{gs.folderID},
	}

	res, err := gs.service.Files.Create(driveFile).SupportsAllDrives(true).SupportsTeamDrives(true).Media(r).Do()
	if err != nil {
		log.Printf("[GDrive] Upload error for key (%s): %v\n", key, err)
		return "", err
	}

	log.Printf("[GDrive] Successfully backed up file (%s) to Google Drive (ID: %s)\n", filename, res.Id)
	return res.Id, nil
}

// DownloadFile downloads an encrypted payload from Google Drive by key.
func (gs *GDriveStore) DownloadFile(key string) (io.ReadCloser, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	if !gs.Enabled || gs.service == nil {
		return nil, fmt.Errorf("Google Drive tier is disabled")
	}

	q := fmt.Sprintf("'%s' in parents and name contains '%s' and trashed = false", gs.folderID, key)
	r, err := gs.service.Files.List().Q(q).SupportsAllDrives(true).SupportsTeamDrives(true).IncludeItemsFromAllDrives(true).Fields("files(id, name)").Do()
	if err != nil || len(r.Files) == 0 {
		return nil, fmt.Errorf("file key (%s) not found on Google Drive", key)
	}

	fileID := r.Files[0].Id
	resp, err := gs.service.Files.Get(fileID).SupportsAllDrives(true).SupportsTeamDrives(true).Download()
	if err != nil {
		return nil, fmt.Errorf("failed to download file from Google Drive: %w", err)
	}

	return resp.Body, nil
}

// DeleteFile removes a file from Google Drive by key.
func (gs *GDriveStore) DeleteFile(key string) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if !gs.Enabled || gs.service == nil {
		return nil
	}

	q := fmt.Sprintf("'%s' in parents and name contains '%s' and trashed = false", gs.folderID, key)
	r, err := gs.service.Files.List().Q(q).SupportsAllDrives(true).SupportsTeamDrives(true).IncludeItemsFromAllDrives(true).Fields("files(id, name)").Do()
	if err != nil || len(r.Files) == 0 {
		return nil
	}

	for _, file := range r.Files {
		if err := gs.service.Files.Delete(file.Id).SupportsAllDrives(true).SupportsTeamDrives(true).Do(); err != nil {
			log.Printf("[GDrive] Error deleting file (%s) from Google Drive: %v\n", file.Name, err)
		} else {
			log.Printf("[GDrive] Deleted file (%s) from Google Drive\n", file.Name)
		}
	}

	return nil
}

func getOrCreateFolder(srv *drive.Service, folderName string) (string, error) {
	q := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", folderName)
	r, err := srv.Files.List().Q(q).SupportsAllDrives(true).SupportsTeamDrives(true).IncludeItemsFromAllDrives(true).Fields("files(id, name)").Do()
	if err == nil && len(r.Files) > 0 {
		return r.Files[0].Id, nil
	}

	qLower := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", strings.ToLower(folderName))
	rLower, errLower := srv.Files.List().Q(qLower).SupportsAllDrives(true).SupportsTeamDrives(true).IncludeItemsFromAllDrives(true).Fields("files(id, name)").Do()
	if errLower == nil && len(rLower.Files) > 0 {
		return rLower.Files[0].Id, nil
	}

	folder := &drive.File{
		Name:     folderName,
		MimeType: "application/vnd.google-apps.folder",
	}

	res, err := srv.Files.Create(folder).SupportsAllDrives(true).SupportsTeamDrives(true).Fields("id").Do()
	if err != nil {
		return "", err
	}

	return res.Id, nil
}
