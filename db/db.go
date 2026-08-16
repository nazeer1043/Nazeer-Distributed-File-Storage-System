package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
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
