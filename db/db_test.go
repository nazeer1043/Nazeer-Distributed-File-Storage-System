package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

func TestDBWrapper_InactiveFallback(t *testing.T) {
	// When DB is inactive or nil, all operations should succeed gracefully without panicking
	wrapper := &DBWrapper{DB: nil, Active: false}

	// User operations
	user, err := wrapper.GetUserByUsername("nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, user)

	err = wrapper.CreateUser(&UserRecord{
		Username:     "test@user.com",
		PasswordHash: "dummyhash",
		FullName:     "Test User",
	})
	assert.NoError(t, err)

	// Session operations
	err = wrapper.CreateSession("token123", "user123", time.Now().Add(time.Hour))
	assert.NoError(t, err)

	sessionUser, err := wrapper.GetSessionUser("token123")
	assert.NoError(t, err)
	assert.Nil(t, sessionUser)

	err = wrapper.DeleteSession("token123")
	assert.NoError(t, err)

	// File record operations
	err = wrapper.SaveFileRecord(&fileserver.FileMeta{
		Key:      "file_key_1",
		Filename: "test.txt",
		Size:     1024,
	})
	assert.NoError(t, err)

	records, err := wrapper.GetFileRecords()
	assert.NoError(t, err)
	assert.Nil(t, records)

	err = wrapper.DeleteFileRecord("file_key_1")
	assert.NoError(t, err)

	// Node operations
	err = wrapper.UpsertNodeRecord("node-3000", "Node 3000", ":3000", "Bootstrap Leader", "Online", 4000000000, 1024)
	assert.NoError(t, err)

	// Audit logs
	assert.NotPanics(t, func() {
		wrapper.InsertAuditLog("INFO", "node-3000", "test audit log")
	})

	logs, err := wrapper.GetAuditLogs(10)
	assert.NoError(t, err)
	assert.Nil(t, logs)
}

func TestConnect_InvalidHostFallback(t *testing.T) {
	// Attempt to connect to an unreachable port/host
	cfg := &config.Config{
		DBUser: "invalid_user",
		DBPass: "invalid_pass",
		DBHost: "127.0.0.1",
		DBPort: "59999", // non-existent port
		DBName: "invalid_db",
	}

	wrapper := Connect(cfg)
	assert.NotNil(t, wrapper)
	// Database should gracefully mark itself inactive when connection fails
	assert.False(t, wrapper.Active)
}

