package db

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Open opens the SQLite database at path with sensible pragmas for concurrent
// use. It is safe to call from both the CLI archiver and the web UI server
// pointing at the same .db file.
func Open(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", path+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	// Single writer keeps SQLite from serialisation errors.
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("cannot connect to database: %w", err)
	}

	// Run pragmas explicitly — modernc.org/sqlite ignores some DSN params.
	pragmas := []string{
		`PRAGMA foreign_keys = ON`,
		// WAL mode allows concurrent reads while the CLI archiver is writing.
		`PRAGMA journal_mode = WAL`,
		// Wait up to 5 s before returning SQLITE_BUSY on a write conflict.
		`PRAGMA busy_timeout = 5000`,
	}
	for _, p := range pragmas {
		if _, err := database.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
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

		// Audit log: records every trash/restore/permanent-delete action on a
		// file. file_id is set to NULL when the registry record is removed so
		// the history survives the file's deletion.
		`CREATE TABLE IF NOT EXISTS file_actions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id      INTEGER REFERENCES file_registry(id) ON DELETE SET NULL,
			file_name    TEXT    NOT NULL,
			archive_path TEXT    NOT NULL,
			action       TEXT    NOT NULL,
			performed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			notes        TEXT
		)`,

		`CREATE INDEX IF NOT EXISTS idx_file_actions_file   ON file_actions(file_id)`,
		`CREATE INDEX IF NOT EXISTS idx_file_actions_action ON file_actions(action)`,
		`CREATE INDEX IF NOT EXISTS idx_file_actions_time   ON file_actions(performed_at)`,
	}

	for _, stmt := range stmts {
		if _, err := database.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// Add trash columns to file_registry if they don't exist yet.
	// SQLite does not support IF NOT EXISTS on ALTER TABLE, so we attempt each
	// and ignore "duplicate column name" errors.
	trashCols := []string{
		`ALTER TABLE file_registry ADD COLUMN trashed_at   DATETIME DEFAULT NULL`,
		`ALTER TABLE file_registry ADD COLUMN trash_path   TEXT     DEFAULT NULL`,
		`ALTER TABLE file_registry ADD COLUMN restore_path TEXT     DEFAULT NULL`,
	}
	for _, stmt := range trashCols {
		if _, err := database.Exec(stmt); err != nil {
			// "duplicate column name" means the column already exists — safe to skip.
			if !isDuplicateColumn(err) {
				return fmt.Errorf("migration failed: %w", err)
			}
		}
	}

	if _, err := database.Exec(
		`CREATE INDEX IF NOT EXISTS idx_registry_trashed ON file_registry(trashed_at)`,
	); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// isDuplicateColumn reports whether err is a SQLite "duplicate column name" error.
func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}
