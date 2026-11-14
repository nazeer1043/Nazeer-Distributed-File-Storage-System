package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/yigithankarabulut/distributed-file-storage/config"
	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

// DBWrapper wraps the *sql.DB connection instance.
type DBWrapper struct {
	*sql.DB
	Active bool
}

// Connect initializes a connection to the MySQL database.
func Connect(cfg *config.Config) *DBWrapper {
	// Build MySQL DSN: user:pass@tcp(host:port)/dbname?parseTime=true
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Printf("[MySQL] Warning: Failed to initialize MySQL driver: %v\n", err)
		return &DBWrapper{DB: nil, Active: false}
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Printf("[MySQL] Warning: Could not connect to MySQL database at %s:%s/%s (%v). Running without database persistence.\n",
			cfg.DBHost, cfg.DBPort, cfg.DBName, err)
		return &DBWrapper{DB: db, Active: false}
	}

	log.Printf("[MySQL] Successfully connected to database: %s@tcp(%s:%s)/%s\n",
		cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName)
	return &DBWrapper{DB: db, Active: true}
}

// SaveFileRecord inserts or updates a file record in the files table.
func (db *DBWrapper) SaveFileRecord(meta *fileserver.FileMeta) error {
	if !db.Active || db.DB == nil {
		return nil
	}

	query := `
		INSERT INTO files (id, file_key, filename, file_size, content_type, owner_name, checksum, encryption_algorithm, status, created_at)
		VALUES (UUID(), ?, ?, ?, ?, ?, ?, 'AES-256-GCM', 'Healthy', NOW())
		ON DUPLICATE KEY UPDATE
			filename = VALUES(filename),
			file_size = VALUES(file_size),
			content_type = VALUES(content_type),
			updated_at = NOW();
	`

	_, err := db.Exec(query, meta.Key, meta.Filename, meta.Size, meta.ContentType, meta.Owner, meta.Checksum)
	if err != nil {
		log.Printf("[MySQL] Error saving file record (%s): %v\n", meta.Filename, err)
		return err
	}

	log.Printf("[MySQL] Saved file metadata (%s) to MySQL files table\n", meta.Filename)
	return nil
}

// DeleteFileRecord removes a file record from the files table by key.
func (db *DBWrapper) DeleteFileRecord(key string) error {
	if !db.Active || db.DB == nil {
		return nil
	}

	query := `DELETE FROM files WHERE file_key = ?`
	_, err := db.Exec(query, key)
	if err != nil {
		log.Printf("[MySQL] Error deleting file record (%s): %v\n", key, err)
		return err
	}

	log.Printf("[MySQL] Purged file (%s) from MySQL files table\n", key)
	return nil
}

// InsertAuditLog records a cluster system log in the system_logs table.
func (db *DBWrapper) InsertAuditLog(level, nodeID, message string) {
	if !db.Active || db.DB == nil {
		return
	}

	query := `INSERT INTO system_logs (level, node_id, message) VALUES (?, ?, ?)`
	_, _ = db.Exec(query, level, nodeID, message)
}

// UserRecord represents a user in the MySQL database.
type UserRecord struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	FullName     string    `json:"full_name"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// GetUserByUsername retrieves a user record by username from the users table.
func (db *DBWrapper) GetUserByUsername(username string) (*UserRecord, error) {
	if !db.Active || db.DB == nil {
		return nil, nil
	}

	query := `SELECT id, username, password_hash, full_name, role, created_at FROM users WHERE username = ? LIMIT 1`
	row := db.QueryRow(query, username)

	var u UserRecord
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		log.Printf("[MySQL] Error retrieving user (%s): %v\n", username, err)
		return nil, err
	}
	return &u, nil
}

// CreateUser inserts a new user record into the users table.
func (db *DBWrapper) CreateUser(u *UserRecord) error {
	if !db.Active || db.DB == nil {
		return nil
	}

	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	if u.Role == "" {
		u.Role = "User"
	}

	query := `INSERT INTO users (id, username, password_hash, full_name, role, created_at) VALUES (?, ?, ?, ?, ?, NOW())`
	_, err := db.Exec(query, u.ID, u.Username, u.PasswordHash, u.FullName, u.Role)
	if err != nil {
		log.Printf("[MySQL] Error creating user (%s): %v\n", u.Username, err)
		return err
	}

	log.Printf("[MySQL] Created user (%s) in MySQL users table\n", u.Username)
	return nil
}

// CreateSession records an active user session in the sessions table.
func (db *DBWrapper) CreateSession(token, userID string, expiresAt time.Time) error {
	if !db.Active || db.DB == nil {
		return nil
	}

	query := `
		INSERT INTO sessions (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at)
	`
	_, err := db.Exec(query, token, userID, expiresAt)
	if err != nil {
		log.Printf("[MySQL] Error creating session: %v\n", err)
		return err
	}
	return nil
}

// GetSessionUser validates a session token and returns the corresponding UserRecord.
func (db *DBWrapper) GetSessionUser(token string) (*UserRecord, error) {
	if !db.Active || db.DB == nil {
		return nil, nil
	}

	query := `
		SELECT u.id, u.username, u.password_hash, u.full_name, u.role, u.created_at
		FROM sessions s
		INNER JOIN users u ON s.user_id = u.id
		WHERE s.token = ? AND s.expires_at > NOW()
		LIMIT 1
	`
	row := db.QueryRow(query, token)

	var u UserRecord
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		log.Printf("[MySQL] Error retrieving session user: %v\n", err)
		return nil, err
	}
	return &u, nil
}

// DeleteSession removes a session from the sessions table by token.
func (db *DBWrapper) DeleteSession(token string) error {
	if !db.Active || db.DB == nil {
		return nil
	}

	query := `DELETE FROM sessions WHERE token = ?`
	_, err := db.Exec(query, token)
	if err != nil {
		log.Printf("[MySQL] Error deleting session: %v\n", err)
		return err
	}
	return nil
}

// GetFileRecords retrieves all stored file metadata from the MySQL database.
func (db *DBWrapper) GetFileRecords() ([]*fileserver.FileMeta, error) {
	if !db.Active || db.DB == nil {
		return nil, nil
	}

	query := `SELECT file_key, filename, file_size, content_type, owner_name, checksum, created_at FROM files ORDER BY created_at DESC`
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("[MySQL] Error querying files: %v\n", err)
		return nil, err
	}
	defer rows.Close()

	var list []*fileserver.FileMeta
	for rows.Next() {
		var m fileserver.FileMeta
		if err := rows.Scan(&m.Key, &m.Filename, &m.Size, &m.ContentType, &m.Owner, &m.Checksum, &m.UploadTime); err != nil {
			continue
		}
		m.Replicas = 3
		list = append(list, &m)
	}
	return list, nil
}

// UpsertNodeRecord creates or updates cluster node information in the nodes table.
func (db *DBWrapper) UpsertNodeRecord(id, name, address, role, status string, totalBytes, usedBytes int64) error {
	if !db.Active || db.DB == nil {
		return nil
	}

	query := `
		INSERT INTO nodes (id, name, address, role, status, total_bytes, used_bytes, last_heartbeat, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NOW(), NOW(), NOW())
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			address = VALUES(address),
			role = VALUES(role),
			status = VALUES(status),
			total_bytes = VALUES(total_bytes),
			used_bytes = VALUES(used_bytes),
			last_heartbeat = NOW(),
			updated_at = NOW();
	`
	_, err := db.Exec(query, id, name, address, role, status, totalBytes, usedBytes)
	if err != nil {
		log.Printf("[MySQL] Error updating node record (%s): %v\n", id, err)
		return err
	}
	return nil
}

// LogRecord represents a log entry retrieved from MySQL.
type LogRecord struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// GetAuditLogs retrieves recent audit logs from MySQL.
func (db *DBWrapper) GetAuditLogs(limit int) ([]LogRecord, error) {
	if !db.Active || db.DB == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT created_at, level, message FROM system_logs ORDER BY created_at DESC LIMIT ?`
	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []LogRecord
	for rows.Next() {
		var lr LogRecord
		var createdAt time.Time
		if err := rows.Scan(&createdAt, &lr.Level, &lr.Message); err != nil {
			continue
		}
		lr.Timestamp = createdAt.Format("15:04:05")
		logs = append(logs, lr)
	}
	return logs, nil
}

