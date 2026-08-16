package config

import (
	"os"
	"strconv"
)

// Config holds the application runtime configuration.
type Config struct {
	HTTPPort       string
	NodePorts      []string
	StorageRoots   []string
	BootstrapNodes []string
	SecretKey      string
	TLSEnabled     bool
	DBUser         string
	DBPass         string
	DBHost         string
	DBPort         string
	DBName         string
}

// LoadConfig loads configuration from environment variables or sensible defaults.
func LoadConfig() *Config {
	httpPort := getEnv("HTTP_PORT", "8080")
	secretKey := getEnv("SECRET_KEY", "nazeerdfs_super_secret_production_key_2026")
	tlsEnabled, _ := strconv.ParseBool(getEnv("TLS_ENABLED", "false"))

	dbUser := getEnv("DB_USER", "root")
	dbPass := getEnv("DB_PASS", "")
	dbHost := getEnv("DB_HOST", "127.0.0.1")
	dbPort := getEnv("DB_PORT", "3306")
	dbName := getEnv("DB_NAME", "nazeerdfs")

	return &Config{
		HTTPPort:       httpPort,
		NodePorts:      []string{"3000", "4000", "5000"},
		StorageRoots:   []string{"3000_network", "4000_network", "5000_network"},
		BootstrapNodes: []string{":3000", ":4000"},
		SecretKey:      secretKey,
		TLSEnabled:     tlsEnabled,
		DBUser:         dbUser,
		DBPass:         dbPass,
		DBHost:         dbHost,
		DBPort:         dbPort,
		DBName:         dbName,
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}
