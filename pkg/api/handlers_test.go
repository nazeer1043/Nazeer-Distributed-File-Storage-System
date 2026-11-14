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
	"github.com/yigithankarabulut/distributed-file-storage/config"
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

func TestApp_ContentAddressedStorage_DeduplicationAndRefCounting(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	app := &App{
		Servers:   []*fileserver.FileServer{srv},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		StartTime: time.Now(),
	}

	aliceSession, err := globalSessions.create(User{Username: "alice_cas", Role: "user"})
	require.NoError(t, err)

	bobSession, err := globalSessions.create(User{Username: "bob_cas", Role: "user"})
	require.NoError(t, err)

	identicalPayload := []byte("Identical content payload for Content Addressable Storage verification 2026")

	// Helper to perform multipart upload
	uploadFile := func(filename string, session string) UploadResponse {
		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = part.Write(identicalPayload)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
		rec := httptest.NewRecorder()

		app.UploadHandler(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp UploadResponse
		err = json.NewDecoder(rec.Body).Decode(&resp)
		require.NoError(t, err)
		return resp
	}

	// 1. Alice uploads doc_1.txt
	aliceResp1 := uploadFile("doc_1.txt", aliceSession)
	assert.True(t, aliceResp1.Success)
	assert.Len(t, aliceResp1.FileID, 64, "Key must be full 64-char SHA-256 hash")

	// 2. Bob uploads same content -> Key must be DIFFERENT (namespaced per owner)
	bobResp := uploadFile("bob_doc.txt", bobSession)
	assert.True(t, bobResp.Success)
	assert.Len(t, bobResp.FileID, 64)
	assert.NotEqual(t, aliceResp1.FileID, bobResp.FileID, "CAS key must be namespaced per owner")

	// 3. Alice uploads doc_2.txt with identical content -> Deduplicated!
	aliceResp2 := uploadFile("doc_2.txt", aliceSession)
	assert.True(t, aliceResp2.Success)
	assert.Equal(t, aliceResp1.FileID, aliceResp2.FileID, "Identical content for same owner must produce identical CAS key")

	// Verify reference count in metadata is 2
	meta, exists := srv.Metadata.Get(aliceResp1.FileID)
	require.True(t, exists)
	assert.Equal(t, 2, meta.RefCount, "Deduplication must increment RefCount to 2")

	// 4. First delete: reference count drops to 1, file remains on disk
	delReq1 := httptest.NewRequest(http.MethodDelete, "/api/delete?key="+aliceResp1.FileID, nil)
	delReq1.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	delRec1 := httptest.NewRecorder()
	app.DeleteHandler(delRec1, delReq1)
	assert.Equal(t, http.StatusOK, delRec1.Code)

	metaAfterFirstDel, exists := srv.Metadata.Get(aliceResp1.FileID)
	require.True(t, exists, "File metadata must still exist after first delete")
	assert.Equal(t, 1, metaAfterFirstDel.RefCount, "RefCount must be decremented to 1")

	// Verify file is still downloadable
	dlReq := httptest.NewRequest(http.MethodGet, "/api/download?key="+aliceResp1.FileID, nil)
	dlReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	dlRec := httptest.NewRecorder()
	app.DownloadHandler(dlRec, dlReq)
	assert.Equal(t, http.StatusOK, dlRec.Code)
	assert.Equal(t, identicalPayload, dlRec.Body.Bytes())

	// 5. Second delete: reference count drops to 0, file is purged
	delReq2 := httptest.NewRequest(http.MethodDelete, "/api/delete?key="+aliceResp1.FileID, nil)
	delReq2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	delRec2 := httptest.NewRecorder()
	app.DeleteHandler(delRec2, delReq2)
	assert.Equal(t, http.StatusOK, delRec2.Code)

	_, exists = srv.Metadata.Get(aliceResp1.FileID)
	assert.False(t, exists, "File metadata must be gone after final delete")

	// Subsequent download must return 404
	dlReq2 := httptest.NewRequest(http.MethodGet, "/api/download?key="+aliceResp1.FileID, nil)
	dlReq2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: aliceSession})
	dlRec2 := httptest.NewRecorder()
	app.DownloadHandler(dlRec2, dlReq2)
	assert.Equal(t, http.StatusNotFound, dlRec2.Code)
}

func TestApp_MaxUploadSizeEnforcement(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	// Configure small max upload size: 20 bytes
	smallCfg := &config.Config{
		MaxUploadSize: 20,
	}

	app := &App{
		Servers:   []*fileserver.FileServer{srv},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		Cfg:       smallCfg,
		StartTime: time.Now(),
	}

	session, err := globalSessions.create(User{Username: "test_size_user", Role: "user"})
	require.NoError(t, err)

	// Create payload larger than 20 bytes (e.g. 100 bytes)
	largePayload := bytes.Repeat([]byte("A"), 100)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "large.txt")
	require.NoError(t, err)
	_, err = part.Write(largePayload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()

	app.UploadHandler(rec, req)
	// Must fail with 400 Bad Request or error due to max bytes exceeded
	assert.NotEqual(t, http.StatusOK, rec.Code)
}

func TestApp_NodeRouting_ConsistentHashingAndRoundRobin(t *testing.T) {
	srv1, cleanup1 := createTestServer(t)
	defer cleanup1()
	srv2, cleanup2 := createTestServer(t)
	defer cleanup2()

	app := &App{
		Servers:   []*fileserver.FileServer{srv1, srv2},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		StartTime: time.Now(),
	}

	// 1. Healthy servers test
	healthy := app.GetHealthyServers()
	assert.Len(t, healthy, 2)

	// 2. Consistent hashing returns deterministic node for the same key
	key := "test-consistent-cas-key-xyz"
	chosen1, err1 := app.SelectServer(key)
	require.NoError(t, err1)
	chosen2, err2 := app.SelectServer(key)
	require.NoError(t, err2)
	assert.Equal(t, chosen1.ListenAddr, chosen2.ListenAddr, "Same key must consistently map to same primary node")

	// 3. Round-robin alternates between healthy nodes
	rr1, errRR1 := app.RoundRobinServer()
	require.NoError(t, errRR1)
	rr2, errRR2 := app.RoundRobinServer()
	require.NoError(t, errRR2)
	assert.NotNil(t, rr1)
	assert.NotNil(t, rr2)

	// 4. Marking a server as stopped excludes it from healthy pool
	srv1.Stop()
	healthyAfterStop := app.GetHealthyServers()
	assert.Len(t, healthyAfterStop, 1)
	assert.Equal(t, srv2.ListenAddr, healthyAfterStop[0].ListenAddr)
}
