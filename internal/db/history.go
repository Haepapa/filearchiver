package db

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// HistoryEntry is a row from the history table (or a synthesised entry from
// file_actions). The Source field distinguishes the two origins.
type HistoryEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	JobName   string    `json:"job_name"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Source    string    `json:"source,omitempty"` // "archive" | "web_ui"
}

// HistoryListParams holds filter/pagination options for ListHistory.
type HistoryListParams struct {
	JobName       string // LIKE search on job_name
	Status        string
	From          string // ISO date "YYYY-MM-DD"
	To            string
	MessageSearch string // LIKE search on message
	Page          int
	PerPage       int
}

// HistoryListResult is the paginated response for ListHistory.
type HistoryListResult struct {
	Entries    []HistoryEntry `json:"entries"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PerPage    int            `json:"per_page"`
	TotalPages int            `json:"total_pages"`
}

// ListHistory returns a paginated, optionally filtered history log.
func ListHistory(database *sql.DB, p HistoryListParams) (*HistoryListResult, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 || p.PerPage > 200 {
		p.PerPage = 50
	}

	var conditions []string
	var args []interface{}

	if p.JobName != "" {
		conditions = append(conditions, `job_name LIKE ?`)
		args = append(args, "%"+p.JobName+"%")
	}
	if p.MessageSearch != "" {
		conditions = append(conditions, `message LIKE ?`)
		args = append(args, "%"+p.MessageSearch+"%")
	}
	if p.Status != "" {
		conditions = append(conditions, `status = ?`)
		args = append(args, p.Status)
	}
	if p.From != "" {
		conditions = append(conditions, `timestamp >= ?`)
		args = append(args, p.From)
	}
	if p.To != "" {
		conditions = append(conditions, `timestamp <= ?`)
		args = append(args, p.To+" 23:59:59")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinConditions(conditions)
	}

	var total int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM history %s", where)
	if err := database.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("history count: %w", err)
	}

	offset := (p.Page - 1) * p.PerPage
	querySQL := fmt.Sprintf(
		`SELECT id, timestamp, job_name, status, message FROM history %s ORDER BY id DESC LIMIT ? OFFSET ?`,
		where,
	)
	queryArgs := append(args, p.PerPage, offset)

	rows, err := database.Query(querySQL, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("history query: %w", err)
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var tsStr string
		if err := rows.Scan(&e.ID, &tsStr, &e.JobName, &e.Status, &e.Message); err != nil {
			return nil, err
		}
		e.Timestamp = parseTime(tsStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalPages := int((total + int64(p.PerPage) - 1) / int64(p.PerPage))
	if totalPages < 1 {
		totalPages = 1
	}

	return &HistoryListResult{
		Entries:    entries,
		Total:      total,
		Page:       p.Page,
		PerPage:    p.PerPage,
		TotalPages: totalPages,
	}, nil
}

func joinConditions(conds []string) string {
	result := ""
	for i, c := range conds {
		if i > 0 {
			result += " AND "
		}
		result += c
	}
	return result
}

// GetHistoryForFile returns a unified timeline of archive history entries and
// web-UI file actions for the given file. CLI archive events are matched by
// archive path; web-UI actions are looked up by file ID.
func GetHistoryForFile(database *sql.DB, fileID int64, archivePath string) ([]HistoryEntry, error) {
	// 1. CLI archive history — match by path substring in the message.
	rows, err := database.Query(`
		SELECT id, timestamp, job_name, status, message
		FROM history
		WHERE message LIKE ?
		ORDER BY id DESC
		LIMIT 20
	`, "%"+archivePath+"%")
	if err != nil {
		return nil, fmt.Errorf("file history query: %w", err)
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var tsStr string
		if err := rows.Scan(&e.ID, &tsStr, &e.JobName, &e.Status, &e.Message); err != nil {
			return nil, err
		}
		e.Timestamp = parseTime(tsStr)
		e.Source = "archive"
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Web-UI file actions — exact match on file_id.
	if fileID > 0 {
		arows, err := database.Query(`
			SELECT id, performed_at, action, COALESCE(notes, '')
			FROM   file_actions
			WHERE  file_id = ?
			ORDER  BY performed_at DESC
			LIMIT  20
		`, fileID)
		if err != nil {
			return nil, fmt.Errorf("file actions query: %w", err)
		}
		defer arows.Close()

		for arows.Next() {
			var a FileAction
			var tsStr string
			if err := arows.Scan(&a.ID, &tsStr, &a.Action, &a.Notes); err != nil {
				return nil, err
			}
			a.PerformedAt = parseTime(tsStr)
			entries = append(entries, HistoryEntry{
				// Use a negative ID to avoid collisions with history table IDs.
				ID:        -a.ID,
				Timestamp: a.PerformedAt,
				JobName:   "web-ui",
				Status:    a.Action,
				Message:   a.Notes,
				Source:    "web_ui",
			})
		}
		if err := arows.Err(); err != nil {
			return nil, err
		}
	}

	// Sort merged results newest first.
	sortHistoryEntries(entries)
	return entries, nil
}

func sortHistoryEntries(entries []HistoryEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
}
