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
	ID           int64     `json:"id"`
	OriginalPath string    `json:"original_path"`
	ArchivePath  string    `json:"archive_path"`
	FileName     string    `json:"file_name"`
	Size         int64     `json:"size"`
	Checksum     string    `json:"checksum"`
	ModTime      time.Time `json:"mod_time"`
	Extension    string    `json:"extension"`
	IsDuplicate  bool      `json:"is_duplicate"`
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
		`SELECT fr.id, fr.original_path, fr.archive_path, fr.file_name, fr.size, fr.checksum, fr.mod_time
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
		if err := rows.Scan(&f.ID, &f.OriginalPath, &f.ArchivePath, &f.FileName, &f.Size, &f.Checksum, &modTimeStr); err != nil {
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
		`SELECT id, original_path, archive_path, file_name, size, checksum, mod_time
		 FROM file_registry WHERE id = ?`, id,
	).Scan(&f.ID, &f.OriginalPath, &f.ArchivePath, &f.FileName, &f.Size, &f.Checksum, &modTimeStr)
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
