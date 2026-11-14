package migrations

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed *.sql
var MigrationFiles embed.FS

// RunMigrations applies all pending versioned schema migrations to the MySQL database.
func RunMigrations(db *sql.DB) error {
	d, err := iofs.New(MigrationFiles, ".")
	if err != nil {
		return fmt.Errorf("failed to create migration source driver: %w", err)
	}

	driver, err := mysql.WithInstance(db, &mysql.Config{})
	if err != nil {
		return fmt.Errorf("failed to create mysql migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", d, "mysql", driver)
	if err != nil {
		return fmt.Errorf("failed to initialize migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration error: %w", err)
	}

	log.Println("[MySQL] Versioned database migrations applied successfully.")
	return nil
}
