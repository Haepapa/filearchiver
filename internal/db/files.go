package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// File is a row from file_registry with derived fields.
type File struct {
	ID           int64      `json:"id"`
	OriginalPath string     `json:"original_path"`
	ArchivePath  string     `json:"archive_path"`
	FileName     string     `json:"file_name"`
	Size         int64      `json:"size"`
	Checksum     string     `json:"checksum"`
	ModTime      time.Time  `json:"mod_time"`
	Extension    string     `json:"extension"`
	IsDuplicate  bool       `json:"is_duplicate"`
	// Trash fields — only populated for trashed files.
	TrashedAt   *time.Time `json:"trashed_at,omitempty"`
	RestorePath string     `json:"restore_path,omitempty"`
	// Proxy fields.
	ProxyPath   string     `json:"proxy_path,omitempty"`
	ProxyStatus string     `json:"proxy_status,omitempty"`
}

// FileListParams holds filter/sort/pagination options for ListFiles.
type FileListParams struct {
	Query          string
	Extension      string
	TagName        string
	From           string // ISO date string "YYYY-MM-DD"
	To             string
	Year           string // 4-digit year, e.g. "2024"
	Month          string // 2-digit month, e.g. "01" (requires Year to be set)
	DuplicatesOnly bool
	Page           int
	PerPage        int
	Sort           string
	Order          string
}

// FileListResult is the paginated response for ListFiles.
type FileListResult struct {
	Files      []File `json:"files"`
	Total      int64  `json:"total"`
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	TotalPages int    `json:"total_pages"`
}

var validSorts = map[string]bool{
	"file_name":    true,
	"size":         true,
	"mod_time":     true,
	"id":           true,
	"archive_path": true,
}

// ListFiles returns a paginated, filtered list of files from file_registry.
func ListFiles(database *sql.DB, p FileListParams) (*FileListResult, error) {
	// Sanitise pagination
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 || p.PerPage > 200 {
		p.PerPage = 50
	}

	// Sanitise sort
	if !validSorts[p.Sort] {
		p.Sort = "mod_time"
	}
	if p.Order != "asc" && p.Order != "desc" {
		p.Order = "desc"
	}

	where, args := buildWhereClause(p)

	countSQL := "SELECT COUNT(*) FROM file_registry fr " + where
	var total int64
	if err := database.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query: %w", err)
	}

	offset := (p.Page - 1) * p.PerPage
	//nolint:gosec // sort and order are validated above
	querySQL := fmt.Sprintf(
		`SELECT fr.id, fr.original_path, fr.archive_path, fr.file_name, fr.size, fr.checksum, fr.mod_time,
		        COALESCE(fr.proxy_path, ''), COALESCE(fr.proxy_status, '')
		 FROM file_registry fr %s
		 ORDER BY fr.%s %s
		 LIMIT ? OFFSET ?`,
		where, p.Sort, p.Order,
	)
	args = append(args, p.PerPage, offset)

	rows, err := database.Query(querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list query: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		var modTimeStr string
		if err := rows.Scan(&f.ID, &f.OriginalPath, &f.ArchivePath, &f.FileName, &f.Size, &f.Checksum, &modTimeStr,
			&f.ProxyPath, &f.ProxyStatus); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		f.ModTime = parseTime(modTimeStr)
		f.Extension = fileExtension(f.FileName)
		f.IsDuplicate = isDuplicate(f.ArchivePath)
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := int((total + int64(p.PerPage) - 1) / int64(p.PerPage))
	if totalPages < 1 {
		totalPages = 1
	}

	return &FileListResult{
		Files:      files,
		Total:      total,
		Page:       p.Page,
		PerPage:    p.PerPage,
		TotalPages: totalPages,
	}, nil
}

// GetFile returns a single file by ID, or nil if not found.
func GetFile(database *sql.DB, id int64) (*File, error) {
	var f File
	var modTimeStr string
	err := database.QueryRow(
		`SELECT id, original_path, archive_path, file_name, size, checksum, mod_time,
		        COALESCE(proxy_path, ''), COALESCE(proxy_status, '')
		 FROM file_registry WHERE id = ?`, id,
	).Scan(&f.ID, &f.OriginalPath, &f.ArchivePath, &f.FileName, &f.Size, &f.Checksum, &modTimeStr,
		&f.ProxyPath, &f.ProxyStatus)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.ModTime = parseTime(modTimeStr)
	f.Extension = fileExtension(f.FileName)
	f.IsDuplicate = isDuplicate(f.ArchivePath)
	return &f, nil
}

// buildWhereClause constructs a SQL WHERE clause (and optional JOIN) from params.
// Returns the clause string and bound argument slice.
func buildWhereClause(p FileListParams) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if p.Query != "" {
		conditions = append(conditions, `(fr.file_name LIKE ? OR fr.original_path LIKE ? OR fr.archive_path LIKE ?)`)
		like := "%" + p.Query + "%"
		args = append(args, like, like, like)
	}

	if p.Extension != "" {
		conditions = append(conditions, `LOWER(fr.file_name) LIKE LOWER(?)`)
		args = append(args, "%."+p.Extension)
	}

	if p.From != "" {
		conditions = append(conditions, `fr.mod_time >= ?`)
		args = append(args, p.From)
	}

	if p.To != "" {
		conditions = append(conditions, `fr.mod_time <= ?`)
		args = append(args, p.To+" 23:59:59")
	}

	// Year and month filtering using mod_time string prefix comparison.
	if p.Year != "" {
		if p.Month != "" {
			conditions = append(conditions, `SUBSTR(fr.mod_time, 1, 7) = ?`)
			args = append(args, p.Year+"-"+p.Month)
		} else {
			conditions = append(conditions, `SUBSTR(fr.mod_time, 1, 4) = ?`)
			args = append(args, p.Year)
		}
	}

	if p.DuplicatesOnly {
		conditions = append(conditions, `fr.archive_path LIKE '%/_duplicates/%'`)
	}

	// Always exclude trashed files from the regular file list.
	conditions = append(conditions, `fr.trashed_at IS NULL`)

	joinClause := ""
	if p.TagName != "" {
		joinClause = `JOIN file_tags ft ON ft.file_id = fr.id JOIN tags t ON t.id = ft.tag_id`
		conditions = append(conditions, `t.name = ?`)
		args = append(args, p.TagName)
	}

	clause := joinClause
	if len(conditions) > 0 {
		clause += " WHERE " + strings.Join(conditions, " AND ")
	}
	return clause, args
}

func fileExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	return strings.TrimPrefix(ext, ".")
}

func isDuplicate(archivePath string) bool {
	return strings.Contains(filepath.ToSlash(archivePath), "/_duplicates/")
}

// TrashFile moves a file's registry record into the "trashed" state.
// trashPath is the new on-disk location (inside _trash/), and the original
// archive_path is saved as restore_path so the file can be restored later.
func TrashFile(database *sql.DB, id int64, trashPath string) error {
	res, err := database.Exec(`
		UPDATE file_registry
		SET    trash_path   = ?,
		       restore_path = archive_path,
		       archive_path = ?,
		       trashed_at   = CURRENT_TIMESTAMP
		WHERE  id = ? AND trashed_at IS NULL`,
		trashPath, trashPath, id,
	)
	if err != nil {
		return fmt.Errorf("trash file: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("file id %d not found or already trashed", id)
	}
	return nil
}

// ListTrashedFiles returns all files currently in the trash, newest first.
func ListTrashedFiles(database *sql.DB) ([]File, error) {
	rows, err := database.Query(`
		SELECT id, original_path, archive_path, file_name, size, checksum,
		       mod_time, trashed_at, restore_path
		FROM   file_registry
		WHERE  trashed_at IS NOT NULL
		ORDER  BY trashed_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list trashed files: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		var modTimeStr, trashedAtStr string
		var restorePath sql.NullString
		if err := rows.Scan(
			&f.ID, &f.OriginalPath, &f.ArchivePath, &f.FileName,
			&f.Size, &f.Checksum, &modTimeStr, &trashedAtStr, &restorePath,
		); err != nil {
			return nil, fmt.Errorf("scan trashed row: %w", err)
		}
		f.ModTime = parseTime(modTimeStr)
		t := parseTime(trashedAtStr)
		f.TrashedAt = &t
		f.RestorePath = restorePath.String
		f.Extension = fileExtension(f.FileName)
		files = append(files, f)
	}
	return files, rows.Err()
}

// RestoreFileRecord clears the trash fields on a file_registry row and returns
// the restore_path so the caller can move the file back on disk.
func RestoreFileRecord(database *sql.DB, id int64) (restorePath string, err error) {
	if err = database.QueryRow(
		`SELECT restore_path FROM file_registry WHERE id = ? AND trashed_at IS NOT NULL`, id,
	).Scan(&restorePath); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("file id %d not found in trash", id)
		}
		return "", fmt.Errorf("restore record lookup: %w", err)
	}

	_, err = database.Exec(`
		UPDATE file_registry
		SET    archive_path = restore_path,
		       trash_path   = NULL,
		       restore_path = NULL,
		       trashed_at   = NULL
		WHERE  id = ?`, id,
	)
	if err != nil {
		return "", fmt.Errorf("restore file record: %w", err)
	}
	return restorePath, nil
}

// parseTime handles the varying datetime formats SQLite may return.
func parseTime(s string) time.Time {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
