package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens the SQLite database at path with foreign keys enabled.
func Open(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", path+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	// SQLite performs best with a single writer connection.
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("cannot connect to database: %w", err)
	}
	// Ensure foreign key constraints are enforced (modernc.org/sqlite ignores
	// the DSN pragma on some versions).
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	return database, nil
}

// Migrate creates the web-UI-specific tables and indexes if they do not already
// exist. It is safe to call on every startup; it never modifies existing data.
func Migrate(database *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tag_categories (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT    NOT NULL UNIQUE,
			color      TEXT    NOT NULL DEFAULT '#6b7280',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`INSERT OR IGNORE INTO tag_categories (name, color) VALUES
			('People',   '#3b82f6'),
			('Places',   '#10b981'),
			('Projects', '#f59e0b')`,

		`CREATE TABLE IF NOT EXISTS tags (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT    NOT NULL,
			category_id INTEGER REFERENCES tag_categories(id) ON DELETE SET NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (name, category_id)
		)`,

		`CREATE TABLE IF NOT EXISTS file_tags (
			file_id    INTEGER NOT NULL REFERENCES file_registry(id) ON DELETE CASCADE,
			tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (file_id, tag_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_file_tags_file ON file_tags(file_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_tags_tag  ON file_tags(tag_id)`,
		`CREATE INDEX IF NOT EXISTS idx_registry_path  ON file_registry(archive_path)`,
	}

	for _, stmt := range stmts {
		if _, err := database.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}
	return nil
}
