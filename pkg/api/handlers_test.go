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

func TestApp_ClusterAndNodesHandlers(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	app := &App{
		Servers:   []*fileserver.FileServer{srv},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		StartTime: time.Now().Add(-10 * time.Minute),
	}

	// 1. Test ClusterHandler
	req := httptest.NewRequest(http.MethodGet, "/api/cluster", nil)
	rec := httptest.NewRecorder()
	app.ClusterHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var clusterResp ClusterStatusResponse
	err := json.NewDecoder(rec.Body).Decode(&clusterResp)
	require.NoError(t, err)
	assert.Equal(t, "nazeerdfs-cluster-01", clusterResp.ClusterID)
	assert.Equal(t, "Healthy", clusterResp.Status)
	assert.Equal(t, 400.0, clusterResp.TotalStorage)

	// 2. Test NodesHandler
	req = httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	rec = httptest.NewRecorder()
	app.NodesHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var nodesResp []NodeInfo
	err = json.NewDecoder(rec.Body).Decode(&nodesResp)
	require.NoError(t, err)
	assert.Len(t, nodesResp, 1)
	assert.Equal(t, "Bootstrap Leader", nodesResp[0].Role)
}

func TestApp_LogsHandler(t *testing.T) {
	app := &App{
		Servers:   []*fileserver.FileServer{},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		StartTime: time.Now(),
	}

	LogEvent("INFO", "Test log message for automated test")

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	app.LogsHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var logs []LogEntry
	err := json.NewDecoder(rec.Body).Decode(&logs)
	require.NoError(t, err)
	assert.NotEmpty(t, logs)
}

func TestApp_UploadDownloadDeleteWorkflow(t *testing.T) {
	srv, cleanup := createTestServer(t)
	defer cleanup()

	app := &App{
		Servers:   []*fileserver.FileServer{srv},
		DB:        &db.DBWrapper{DB: nil, Active: false},
		StartTime: time.Now(),
	}

	fileContent := []byte("NazeerDFS Enterprise Storage verification payload 2026")
	fileName := "report_final.txt"

	// 1. Upload file via multipart form
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", fileName)
	require.NoError(t, err)
	_, err = part.Write(fileContent)
	require.NoError(t, err)
	_ = writer.WriteField("owner", "Cluster Administrator")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()

	app.UploadHandler(uploadRec, uploadReq)
	assert.Equal(t, http.StatusOK, uploadRec.Code)

	var uploadResp UploadResponse
	err = json.NewDecoder(uploadRec.Body).Decode(&uploadResp)
	require.NoError(t, err)
	assert.True(t, uploadResp.Success)
	assert.NotEmpty(t, uploadResp.FileID)
	assert.Equal(t, fileName, uploadResp.Filename)

	// 2. Query /api/files to verify file appears in listing
	filesReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	filesRec := httptest.NewRecorder()
	app.FilesHandler(filesRec, filesReq)

	assert.Equal(t, http.StatusOK, filesRec.Code)
	var fileList []FileItem
	err = json.NewDecoder(filesRec.Body).Decode(&fileList)
	require.NoError(t, err)
	require.Len(t, fileList, 1)
	assert.Equal(t, fileName, fileList[0].Name)
	assert.Equal(t, uploadResp.FileID, fileList[0].Location)

	// 3. Download the file by storage key
	downloadReq := httptest.NewRequest(http.MethodGet, "/api/download?key="+uploadResp.FileID, nil)
	downloadRec := httptest.NewRecorder()
	app.DownloadHandler(downloadRec, downloadReq)

	assert.Equal(t, http.StatusOK, downloadRec.Code)
	downloadedData, err := io.ReadAll(downloadRec.Body)
	require.NoError(t, err)
	assert.Equal(t, fileContent, downloadedData)

	// 4. Download by filename
	downloadByFilenameReq := httptest.NewRequest(http.MethodGet, "/api/download?file="+fileName, nil)
	downloadByFilenameRec := httptest.NewRecorder()
	app.DownloadHandler(downloadByFilenameRec, downloadByFilenameReq)
	assert.Equal(t, http.StatusOK, downloadByFilenameRec.Code)
	assert.Equal(t, fileContent, downloadByFilenameRec.Body.Bytes())

	// 5. Delete file by key
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/delete?key="+uploadResp.FileID, nil)
	deleteRec := httptest.NewRecorder()
	app.DeleteHandler(deleteRec, deleteReq)

	assert.Equal(t, http.StatusOK, deleteRec.Code)
	var delResp DeleteResponse
	err = json.NewDecoder(deleteRec.Body).Decode(&delResp)
	require.NoError(t, err)
	assert.True(t, delResp.Success)

	// 6. Verify file is gone from /api/files
	filesRec2 := httptest.NewRecorder()
	app.FilesHandler(filesRec2, filesReq)
	var fileListAfter []FileItem
	_ = json.NewDecoder(filesRec2.Body).Decode(&fileListAfter)
	assert.Empty(t, fileListAfter)
}
