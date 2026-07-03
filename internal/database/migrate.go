package database

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func RunMigrations(databaseURL string) error {
	migrateURL, err := migrateDatabaseURL(databaseURL)
	if err != nil {
		return fmt.Errorf("prepare migration URL: %w", err)
	}

	m, err := migrate.New(
		"file://migrations",
		migrateURL,
	)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// migrateDatabaseURL rewrites postgres:// URLs to pgx5:// for golang-migrate's pgx v5 driver.
func migrateDatabaseURL(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", err
	}

	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql":
		u.Scheme = "pgx5"
	case "pgx5":
		// already correct
	default:
		return "", fmt.Errorf("unsupported database URL scheme %q for migrations", u.Scheme)
	}

	return u.String(), nil
}
