package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// FileAction is a row from the file_actions audit table.
type FileAction struct {
	ID          int64      `json:"id"`
	FileID      *int64     `json:"file_id"`
	FileName    string     `json:"file_name"`
	ArchivePath string     `json:"archive_path"`
	Action      string     `json:"action"` // "trashed" | "restored" | "permanently_deleted"
	PerformedAt time.Time  `json:"performed_at"`
	Notes       string     `json:"notes,omitempty"`
}

// FileActionListParams holds filter/pagination options for ListAllFileActions.
type FileActionListParams struct {
	Action  string // filter by action value; empty = all
	Search  string // LIKE search on file_name or archive_path
	From    string // ISO date "YYYY-MM-DD"
	To      string
	Page    int
	PerPage int
}

// FileActionListResult is the paginated response for ListAllFileActions.
type FileActionListResult struct {
	Actions    []FileAction `json:"actions"`
	Total      int64        `json:"total"`
	Page       int          `json:"page"`
	PerPage    int          `json:"per_page"`
	TotalPages int          `json:"total_pages"`
}

// LogFileAction inserts a row into the file_actions audit table.
// fileID may be nil when the registry record no longer exists.
func LogFileAction(database *sql.DB, fileID *int64, fileName, archivePath, action, notes string) error {
	_, err := database.Exec(`
		INSERT INTO file_actions (file_id, file_name, archive_path, action, notes)
		VALUES (?, ?, ?, ?, ?)`,
		fileID, fileName, archivePath, action, notes,
	)
	if err != nil {
		return fmt.Errorf("log file action: %w", err)
	}
	return nil
}

// ListFileActions returns all audit actions for a specific file, newest first.
// It matches by file_id when the record still exists, and also by archive_path
// so that pre-trash CLI history and the post-trash path both surface entries.
func ListFileActions(database *sql.DB, fileID int64) ([]FileAction, error) {
	rows, err := database.Query(`
		SELECT id, file_id, file_name, archive_path, action, performed_at,
		       COALESCE(notes, '')
		FROM   file_actions
		WHERE  file_id = ?
		ORDER  BY performed_at DESC`,
		fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("list file actions: %w", err)
	}
	defer rows.Close()
	return scanFileActions(rows)
}

// ListAllFileActions returns a paginated, optionally filtered list of all audit actions.
func ListAllFileActions(database *sql.DB, p FileActionListParams) (*FileActionListResult, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 || p.PerPage > 200 {
		p.PerPage = 50
	}

	var conditions []string
	var args []interface{}

	if p.Action != "" {
		conditions = append(conditions, `action = ?`)
		args = append(args, p.Action)
	}
	if p.Search != "" {
		conditions = append(conditions, `(file_name LIKE ? OR archive_path LIKE ?)`)
		like := "%" + p.Search + "%"
		args = append(args, like, like)
	}
	if p.From != "" {
		conditions = append(conditions, `performed_at >= ?`)
		args = append(args, p.From)
	}
	if p.To != "" {
		conditions = append(conditions, `performed_at <= ?`)
		args = append(args, p.To+" 23:59:59")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM file_actions "+where, args...,
	).Scan(&total); err != nil {
		return nil, fmt.Errorf("file actions count: %w", err)
	}

	offset := (p.Page - 1) * p.PerPage
	queryArgs := append(args, p.PerPage, offset)
	rows, err := database.Query(
		`SELECT id, file_id, file_name, archive_path, action, performed_at,
		        COALESCE(notes, '')
		 FROM   file_actions `+where+`
		 ORDER  BY performed_at DESC
		 LIMIT  ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, fmt.Errorf("file actions query: %w", err)
	}
	defer rows.Close()

	actions, err := scanFileActions(rows)
	if err != nil {
		return nil, err
	}

	totalPages := int((total + int64(p.PerPage) - 1) / int64(p.PerPage))
	if totalPages < 1 {
		totalPages = 1
	}
	return &FileActionListResult{
		Actions:    actions,
		Total:      total,
		Page:       p.Page,
		PerPage:    p.PerPage,
		TotalPages: totalPages,
	}, nil
}

func scanFileActions(rows *sql.Rows) ([]FileAction, error) {
	var actions []FileAction
	for rows.Next() {
		var a FileAction
		var fileID sql.NullInt64
		var tsStr string
		if err := rows.Scan(
			&a.ID, &fileID, &a.FileName, &a.ArchivePath,
			&a.Action, &tsStr, &a.Notes,
		); err != nil {
			return nil, fmt.Errorf("scan file action: %w", err)
		}
		if fileID.Valid {
			v := fileID.Int64
			a.FileID = &v
		}
		a.PerformedAt = parseTime(tsStr)
		actions = append(actions, a)
	}
	return actions, rows.Err()
}
