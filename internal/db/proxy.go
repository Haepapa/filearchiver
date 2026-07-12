package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ProxyStatus values stored in file_registry.proxy_status.
const (
	ProxyStatusPending    = "pending"
	ProxyStatusProcessing = "processing"
	ProxyStatusDone       = "done"
	ProxyStatusFailed     = "failed"
	ProxyStatusSkipped    = "skipped"
)

// ProxySetting is a single key/value proxy configuration row.
type ProxySetting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProxyStats holds queue depth counts returned by GetProxyStats.
type ProxyStats struct {
	Pending    int    `json:"pending"`
	Processing int    `json:"processing"`
	Done       int    `json:"done"`
	Failed     int    `json:"failed"`
	Skipped    int    `json:"skipped"`
	CurrentFile string `json:"current_file,omitempty"`
}

// defaultProxySettings are written on first startup.
var defaultProxySettings = map[string]string{
	"enabled":          "true",
	"paused":           "false",
	"min_file_size_mb": "10",
	"max_workers":      "1",
	"image_max_width":  "2048",
	"image_quality":    "85",
	"video_max_width":  "1280",
	"video_crf":        "28",
	"use_gpu":          "false",
}

// InitProxySettings inserts default settings rows that do not already exist.
func InitProxySettings(db *sql.DB) error {
	for k, v := range defaultProxySettings {
		if _, err := db.Exec(
			`INSERT OR IGNORE INTO proxy_settings (key, value) VALUES (?, ?)`, k, v,
		); err != nil {
			return fmt.Errorf("init proxy setting %q: %w", k, err)
		}
	}
	return nil
}

// GetAllProxySettings returns all rows from proxy_settings as a map.
func GetAllProxySettings(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM proxy_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// SetProxySetting upserts a single proxy setting key/value.
func SetProxySetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(
		`INSERT INTO proxy_settings (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value,
	)
	return err
}

// SetProxySettings upserts multiple proxy settings atomically.
func SetProxySettings(db *sql.DB, settings map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO proxy_settings (key, value, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for k, v := range settings {
		if _, err := stmt.Exec(k, v); err != nil {
			return fmt.Errorf("set proxy setting %q: %w", k, err)
		}
	}
	return tx.Commit()
}

// ProxyQueueItem is a file that needs a proxy generated.
type ProxyQueueItem struct {
	ID          int64
	ArchivePath string
	Extension   string
	FileSize    int64
}

// ListFilesNeedingProxy returns up to limit files that are eligible for proxy
// generation (proxy_status = 'pending') and not trashed.
func ListFilesNeedingProxy(db *sql.DB, limit int) ([]ProxyQueueItem, error) {
	rows, err := db.Query(`
		SELECT id, archive_path, file_name, size
		FROM   file_registry
		WHERE  proxy_status = ?
		AND    trashed_at IS NULL
		ORDER  BY id
		LIMIT  ?
	`, ProxyStatusPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ProxyQueueItem
	for rows.Next() {
		var it ProxyQueueItem
		var fileName string
		if err := rows.Scan(&it.ID, &it.ArchivePath, &fileName, &it.FileSize); err != nil {
			return nil, err
		}
		// Derive extension from file_name (no dedicated column in file_registry).
		it.Extension = fileNameExtension(fileName)
		items = append(items, it)
	}
	return items, rows.Err()
}

// fileNameExtension returns the lowercase extension (without dot) of a filename,
// e.g. "IMG_5432.CR2" → "cr2".  Returns "" for files with no extension.
func fileNameExtension(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return strings.ToLower(name[i+1:])
		}
		if name[i] == '/' || name[i] == '\\' {
			break
		}
	}
	return ""
}

// EnqueueEligibleFiles marks NULL-status files as 'pending' or 'skipped'
// based on the minimum file size. Extension filtering happens at processing
// time in the worker (file_registry has no dedicated extension column).
// Runs once on worker start and whenever settings change.
func EnqueueEligibleFiles(db *sql.DB, minBytes int64, _ map[string]bool) (int64, error) {
	// Mark files above the size threshold as pending.
	res, err := db.Exec(`
		UPDATE file_registry
		SET    proxy_status = ?
		WHERE  proxy_status IS NULL
		AND    trashed_at   IS NULL
		AND    size         >= ?
	`, ProxyStatusPending, minBytes)
	if err != nil {
		return 0, err
	}
	enqueued, _ := res.RowsAffected()

	// Mark all remaining NULL-status files (too small) as skipped.
	if _, err := db.Exec(`
		UPDATE file_registry
		SET    proxy_status = ?
		WHERE  proxy_status IS NULL
		AND    trashed_at   IS NULL
	`, ProxyStatusSkipped); err != nil {
		return enqueued, err
	}
	return enqueued, nil
}

// MarkProxyProcessing sets a file's proxy_status to 'processing'.
func MarkProxyProcessing(db *sql.DB, fileID int64) error {
	_, err := db.Exec(
		`UPDATE file_registry SET proxy_status = ? WHERE id = ?`,
		ProxyStatusProcessing, fileID,
	)
	return err
}

// MarkProxyDone records a successful proxy generation.
func MarkProxyDone(db *sql.DB, fileID int64, proxyPath string) error {
	_, err := db.Exec(
		`UPDATE file_registry
		 SET proxy_status = ?, proxy_path = ?, proxy_generated_at = CURRENT_TIMESTAMP, proxy_error = NULL
		 WHERE id = ?`,
		ProxyStatusDone, proxyPath, fileID,
	)
	return err
}

// MarkProxyFailed records a proxy generation failure.
func MarkProxyFailed(db *sql.DB, fileID int64, errMsg string) error {
	_, err := db.Exec(
		`UPDATE file_registry SET proxy_status = ?, proxy_error = ? WHERE id = ?`,
		ProxyStatusFailed, errMsg, fileID,
	)
	return err
}

// MarkProxySkipped marks a file as skipped (below size threshold or unsupported).
func MarkProxySkipped(db *sql.DB, fileID int64) error {
	_, err := db.Exec(
		`UPDATE file_registry SET proxy_status = ? WHERE id = ?`,
		ProxyStatusSkipped, fileID,
	)
	return err
}

// ResetFailedProxies resets all 'failed' files back to 'pending' so they are
// retried on the next worker pass.
func ResetFailedProxies(db *sql.DB) (int64, error) {
	res, err := db.Exec(
		`UPDATE file_registry SET proxy_status = ?, proxy_error = NULL WHERE proxy_status = ?`,
		ProxyStatusPending, ProxyStatusFailed,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ResetSkippedProxies resets all 'skipped' rows back to NULL so they are
// re-evaluated by EnqueueEligibleFiles. Useful after changing the min file
// size setting or after a bug fix that caused incorrect skipping.
func ResetSkippedProxies(db *sql.DB) error {
	_, err := db.Exec(
		`UPDATE file_registry SET proxy_status = NULL WHERE proxy_status = ?`,
		ProxyStatusSkipped,
	)
	return err
}

// ResetFileProxy resets a single file's proxy columns back to pending so the
// worker will regenerate it. The caller is responsible for deleting the old
// proxy file on disk before calling this.
func ResetFileProxy(db *sql.DB, fileID int64) error {
	_, err := db.Exec(
		`UPDATE file_registry
		 SET proxy_status = ?, proxy_path = NULL, proxy_error = NULL, proxy_generated_at = NULL
		 WHERE id = ?`,
		ProxyStatusPending, fileID,
	)
	return err
}

// ResetProcessingProxies resets any stale 'processing' rows (left over from
// an unclean shutdown) back to 'pending'.
func ResetProcessingProxies(db *sql.DB) error {
	_, err := db.Exec(
		`UPDATE file_registry SET proxy_status = ? WHERE proxy_status = ?`,
		ProxyStatusPending, ProxyStatusProcessing,
	)
	return err
}

// GetProxyStats returns queue depth counts and the archive_path of the file
// currently being processed (if any).
func GetProxyStats(db *sql.DB) (ProxyStats, error) {
	var stats ProxyStats

	rows, err := db.Query(`
		SELECT proxy_status, COUNT(*) FROM file_registry
		WHERE  proxy_status IS NOT NULL
		GROUP  BY proxy_status
	`)
	if err != nil {
		return stats, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		switch status {
		case ProxyStatusPending:
			stats.Pending = count
		case ProxyStatusProcessing:
			stats.Processing = count
		case ProxyStatusDone:
			stats.Done = count
		case ProxyStatusFailed:
			stats.Failed = count
		case ProxyStatusSkipped:
			stats.Skipped = count
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	// Best-effort: find the file currently being processed.
	row := db.QueryRow(`
		SELECT archive_path FROM file_registry
		WHERE  proxy_status = ?
		LIMIT  1
	`, ProxyStatusProcessing)
	_ = row.Scan(&stats.CurrentFile)

	return stats, nil
}
