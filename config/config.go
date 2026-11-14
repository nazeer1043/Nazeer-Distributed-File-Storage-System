package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds the application runtime configuration.
type Config struct {
	Env                    string // "development", "test", "production"
	HTTPPort               string
	NodeListenAddrs        []string
	StorageRoots           []string
	BootstrapNodes         []string
	EncryptionKey          []byte // Decoded 32-byte encryption key
	EncryptionKeyID        string // Key identifier for future key rotation
	ClusterSecret          string // Shared cluster secret for node-to-node auth
	AdminBootstrapUser     string
	AdminBootstrapPassword string

	// Database configuration
	MySQLDSN string
	DBUser   string
	DBPass   string
	DBHost   string
	DBPort   string
	DBName   string

	// Google Drive configuration
	CredentialsJSON string
	TokenJSON       string
	CredentialsPath string
	TokenPath       string

	// Max Upload Size (bytes)
	MaxUploadSize int64

	// TLS / Security
	TLSEnabled bool
}

// LoadConfig loads configuration from environment variables with sensible development defaults.
func LoadConfig() (*Config, error) {
	env := getEnv("APP_ENV", getEnv("ENV", "development"))

	httpPort := getEnv("PORT", getEnv("HTTP_PORT", "8080"))

	rawNodeAddrs := getEnv("NODE_LISTEN_ADDRS", ":3000,:4000,:5000")
	nodePorts := splitCommaTrim(rawNodeAddrs)

	rawRoots := getEnv("STORAGE_ROOTS", "")
	var storageRoots []string
	if rawRoots != "" {
		storageRoots = splitCommaTrim(rawRoots)
	} else {
		for _, addr := range nodePorts {
			clean := strings.TrimPrefix(addr, ":")
			storageRoots = append(storageRoots, clean+"_network")
		}
	}

	rawBootstraps := getEnv("BOOTSTRAP_NODES", ":3000,:4000")
	var bootstrapNodes []string
	if rawBootstraps != "" {
		bootstrapNodes = splitCommaTrim(rawBootstraps)
	}

	clusterSecret := getEnv("CLUSTER_SECRET", "")
	if clusterSecret == "" && env != "production" {
		clusterSecret = "dev-cluster-secret-32-bytes-long!!"
	}

	adminUser := getEnv("ADMIN_BOOTSTRAP_USERNAME", "admin")
	adminPass := getEnv("ADMIN_BOOTSTRAP_PASSWORD", "")
	if adminPass == "" && env != "production" {
		adminPass = "Admin123!SecurePass"
	}

	// Encryption key: load from ENCRYPTION_KEY (base64) or ENCRYPTION_KEY_FILE
	var encKeyBytes []byte
	keyB64 := getEnv("ENCRYPTION_KEY", "")
	keyFile := getEnv("ENCRYPTION_KEY_FILE", "")
	keyID := getEnv("ENCRYPTION_KEY_ID", "k1")

	if keyB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keyB64))
		if err != nil {
			return nil, fmt.Errorf("invalid ENCRYPTION_KEY (must be base64-encoded): %w", err)
		}
		encKeyBytes = decoded
	} else if keyFile != "" {
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read ENCRYPTION_KEY_FILE %q: %w", keyFile, err)
		}
		// If the file is 32 raw bytes or 44 base64 bytes
		trimmed := strings.TrimSpace(string(raw))
		if len(raw) == 32 {
			encKeyBytes = raw
		} else if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(decoded) == 32 {
			encKeyBytes = decoded
		} else {
			return nil, fmt.Errorf("ENCRYPTION_KEY_FILE must contain exactly 32 bytes (raw or base64)")
		}
	} else if env != "production" {
		// Dev default 32-byte key
		encKeyBytes = []byte("12345678901234567890123456789012")
	}

	// Database config
	mysqlDSN := getEnv("MYSQL_DSN", getEnv("DATABASE_URL", ""))
	dbUser := getEnv("DB_USER", "root")
	dbPass := getEnv("DB_PASS", "")
	dbHost := getEnv("DB_HOST", "127.0.0.1")
	dbPort := getEnv("DB_PORT", "3306")
	dbName := getEnv("DB_NAME", "nazeerdfs")

	if mysqlDSN == "" && dbUser != "" && dbHost != "" {
		mysqlDSN = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true", dbUser, dbPass, dbHost, dbPort, dbName)
	}

	credsJSON := getEnv("CREDENTIALS_JSON", "")
	tokenJSON := getEnv("TOKEN_JSON", "")
	credsPath := getEnv("CREDENTIALS_PATH", "credentials.json")
	tokenPath := getEnv("TOKEN_PATH", "token.json")

	tlsEnabled, _ := strconv.ParseBool(getEnv("TLS_ENABLED", "false"))

	maxUploadSize := int64(1024 * 1024 * 1024) // 1GB default
	if maxStr := getEnv("MAX_UPLOAD_SIZE_BYTES", ""); maxStr != "" {
		if parsed, err := strconv.ParseInt(maxStr, 10, 64); err == nil && parsed > 0 {
			maxUploadSize = parsed
		}
	}

	cfg := &Config{
		Env:                    env,
		HTTPPort:               httpPort,
		NodeListenAddrs:        nodePorts,
		StorageRoots:           storageRoots,
		BootstrapNodes:         bootstrapNodes,
		EncryptionKey:          encKeyBytes,
		EncryptionKeyID:        keyID,
		ClusterSecret:          clusterSecret,
		AdminBootstrapUser:     adminUser,
		AdminBootstrapPassword: adminPass,
		MySQLDSN:               mysqlDSN,
		DBUser:                 dbUser,
		DBPass:                 dbPass,
		DBHost:                 dbHost,
		DBPort:                 dbPort,
		DBName:                 dbName,
		CredentialsJSON:        credsJSON,
		TokenJSON:              tokenJSON,
		CredentialsPath:        credsPath,
		TokenPath:              tokenPath,
		MaxUploadSize:          maxUploadSize,
		TLSEnabled:             tlsEnabled,
	}

	return cfg, nil
}

// Validate validates configuration and enforces strict security in production.
func (c *Config) Validate() error {
	if c.HTTPPort == "" {
		return errors.New("HTTP_PORT is required")
	}
	if len(c.NodeListenAddrs) == 0 {
		return errors.New("at least one node listen address is required (NODE_LISTEN_ADDRS)")
	}
	if len(c.StorageRoots) != len(c.NodeListenAddrs) {
		return fmt.Errorf("number of storage roots (%d) must match number of node listen addresses (%d)",
			len(c.StorageRoots), len(c.NodeListenAddrs))
	}

	if len(c.EncryptionKey) != 32 {
		return fmt.Errorf("ENCRYPTION_KEY must be exactly 32 bytes (got %d)", len(c.EncryptionKey))
	}

	if c.Env == "production" {
		if c.ClusterSecret == "" || len(c.ClusterSecret) < 16 {
			return errors.New("CLUSTER_SECRET must be at least 16 characters in production")
		}
		if c.AdminBootstrapPassword != "" && len(c.AdminBootstrapPassword) < 12 {
			return errors.New("ADMIN_BOOTSTRAP_PASSWORD must be at least 12 characters")
		}
		if string(c.EncryptionKey) == "12345678901234567890123456789012" {
			return errors.New("default development ENCRYPTION_KEY cannot be used in production; set ENCRYPTION_KEY to a base64-encoded 32-byte secret")
		}
	}

	return nil
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func splitCommaTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
