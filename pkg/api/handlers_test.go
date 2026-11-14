package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yigithankarabulut/distributed-file-storage/crypto"
	"github.com/yigithankarabulut/distributed-file-storage/db"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
	"github.com/yigithankarabulut/distributed-file-storage/p2p"
	"github.com/yigithankarabulut/distributed-file-storage/store"
)

func createTestServer(t *testing.T) (*fileserver.FileServer, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "nazeerdfs_test_*")
	require.NoError(t, err)

	key, err := crypto.NewEncryptionKey()
	require.NoError(t, err)

	tcpTransport := p2p.NewTCPTransport(
		p2p.WithListenAddr(":0"),
		p2p.WithHandshakeFunc(p2p.NOPHandshakeFunc),
		p2p.WithDecoder(&p2p.DefaultDecoder{}),
	)

	opts := fileserver.ServerOpts{
		StorageRoot:       tempDir,
		PathTransformFunc: store.CASPathTransformFunc,
		Transport:         tcpTransport,
		EncryptKey:        key,
	}

	server := fileserver.NewFileServer(opts)

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	return server, cleanup
}

func TestApp_AdminRouteGuards(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	app := &App{
		Servers:   []*fileserver.FileServer{srv},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		StartTime: time.Now().Add(-10 * time.Minute),
	}

	// Register users in globalUsers
	globalUsers.mu.Lock()
	globalUsers.users["alice_user"] = "mockhash"
	globalUsers.roles["alice_user"] = "user"
	globalUsers.users["admin_user"] = "mockhash"
	globalUsers.roles["admin_user"] = "admin"
	globalUsers.mu.Unlock()

	userSession, err := globalSessions.create(User{
		Username: "alice_user",
		Role:     "user",
	})
	require.NoError(t, err)

	adminSession, err := globalSessions.create(User{
		Username: "admin_user",
		Role:     "admin",
	})
	require.NoError(t, err)

	adminRoutes := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"Logs", http.MethodGet, "/api/logs", app.RequireAdmin(app.LogsHandler)},
		{"Nodes", http.MethodGet, "/api/nodes", app.RequireAdmin(app.NodesHandler)},
		{"Cluster", http.MethodGet, "/api/cluster", app.RequireAdmin(app.ClusterHandler)},
		{"Settings", http.MethodGet, "/api/settings", app.RequireAdmin(app.SettingsHandler)},
		{"Users", http.MethodGet, "/api/users", app.RequireAdmin(app.UsersListHandler)},
	}

	for _, route := range adminRoutes {
		t.Run("NonAdmin_Forbidden_"+route.name, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: userSession})
			rec := httptest.NewRecorder()

			route.handler(rec, req)
			assert.Equal(t, http.StatusForbidden, rec.Code, "Regular user must receive 403 on "+route.name)
		})

		t.Run("Admin_Allowed_"+route.name, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: adminSession})
			rec := httptest.NewRecorder()

			route.handler(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, "Admin must receive 200 on "+route.name)
		})
	}
}

func TestApp_CrossUserFileAuthorization(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	app := &App{
		Servers:   []*fileserver.FileServer{srv},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		StartTime: time.Now(),
	}

	// Create sessions for Alice (user), Bob (user), and Charlie (admin)
	aliceSession, err := globalSessions.create(User{
		Username: "alice",
		Role:     "user",
	})
	require.NoError(t, err)

	bobSession, err := globalSessions.create(User{
		Username: "bob",
		Role:     "user",
	})
	require.NoError(t, err)

	adminSession, err := globalSessions.create(User{
		Username: "charlie_admin",
		Role:     "admin",
	})
	require.NoError(t, err)

	fileContent := []byte("Top secret data belonging exclusively to Alice")
	fileName := "../../../traversal_test.txt"

	// 1. Alice uploads her file (form field 'owner' is ignored; session owner is used)
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, err = part.Write(fileContent)
	require.NoError(t, err)
	_ = writer.WriteField("owner", "AttemptedSpoofedOwner")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	uploadRec := httptest.NewRecorder()

	app.UploadHandler(uploadRec, uploadReq)
	require.Equal(t, http.StatusOK, uploadRec.Code)

	var uploadResp UploadResponse
	err = json.NewDecoder(uploadRec.Body).Decode(&uploadResp)
	require.NoError(t, err)
	aliceFileKey := uploadResp.FileID
	require.NotEmpty(t, aliceFileKey)

	// 2. Bob lists files -> Alice's file MUST NOT appear
	bobListReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	bobListReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	bobListRec := httptest.NewRecorder()
	app.FilesHandler(bobListRec, bobListReq)
	require.Equal(t, http.StatusOK, bobListRec.Code)

	var bobFiles []FileItem
	err = json.NewDecoder(bobListRec.Body).Decode(&bobFiles)
	require.NoError(t, err)
	assert.Empty(t, bobFiles, "Bob should see zero files")

	// 3. Alice lists files -> Alice sees her file with owner 'alice'
	aliceListReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	aliceListReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	aliceListRec := httptest.NewRecorder()
	app.FilesHandler(aliceListRec, aliceListReq)
	require.Equal(t, http.StatusOK, aliceListRec.Code)

	var aliceFiles []FileItem
	err = json.NewDecoder(aliceListRec.Body).Decode(&aliceFiles)
	require.NoError(t, err)
	require.Len(t, aliceFiles, 1)
	assert.Equal(t, "alice", aliceFiles[0].Owner)

	// 4. Bob tries to download Alice's file -> 403 Forbidden
	bobDlReq := httptest.NewRequest(http.MethodGet, "/api/download?key="+aliceFileKey, nil)
	bobDlReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	bobDlRec := httptest.NewRecorder()
	app.DownloadHandler(bobDlRec, bobDlReq)
	assert.Equal(t, http.StatusForbidden, bobDlRec.Code)

	// 5. Bob tries to delete Alice's file -> 403 Forbidden
	bobDelReq := httptest.NewRequest(http.MethodDelete, "/api/delete?key="+aliceFileKey, nil)
	bobDelReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	bobDelRec := httptest.NewRecorder()
	app.DeleteHandler(bobDelRec, bobDelReq)
	assert.Equal(t, http.StatusForbidden, bobDelRec.Code)

	// 6. Alice downloads her file -> 200 OK with sanitized filename and nosniff
	aliceDlReq := httptest.NewRequest(http.MethodGet, "/api/download?key="+aliceFileKey, nil)
	aliceDlReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	aliceDlRec := httptest.NewRecorder()
	app.DownloadHandler(aliceDlRec, aliceDlReq)
	assert.Equal(t, http.StatusOK, aliceDlRec.Code)
	assert.Equal(t, "nosniff", aliceDlRec.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, aliceDlRec.Header().Get("Content-Disposition"), "traversal_test.txt")
	assert.NotContains(t, aliceDlRec.Header().Get("Content-Disposition"), "../")

	data, err := io.ReadAll(aliceDlRec.Body)
	require.NoError(t, err)
	assert.Equal(t, fileContent, data)

	// 7. Admin downloads Alice's file -> 200 OK
	adminDlReq := httptest.NewRequest(http.MethodGet, "/api/download?key="+aliceFileKey, nil)
	adminDlReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: adminSession})
	adminDlRec := httptest.NewRecorder()
	app.DownloadHandler(adminDlRec, adminDlReq)
	assert.Equal(t, http.StatusOK, adminDlRec.Code)

	// 8. Alice deletes her file -> 200 OK
	aliceDelReq := httptest.NewRequest(http.MethodDelete, "/api/delete?key="+aliceFileKey, nil)
	aliceDelReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	aliceDelRec := httptest.NewRecorder()
	app.DeleteHandler(aliceDelRec, aliceDelReq)
	assert.Equal(t, http.StatusOK, aliceDelRec.Code)
}
