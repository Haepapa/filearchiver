package db

import (
	"database/sql"
	"fmt"
	"time"
)

// HistoryEntry is a row from the history table.
type HistoryEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	JobName   string    `json:"job_name"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
}

// HistoryListParams holds filter/pagination options for ListHistory.
type HistoryListParams struct {
	JobName string
	Status  string
	From    string
	To      string
	Page    int
	PerPage int
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
		conditions = append(conditions, `job_name = ?`)
		args = append(args, p.JobName)
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

// GetHistoryForFile returns the most recent history entries whose message
// references the given archive path. This surfaces the audit trail for a
// specific file in the viewer metadata sidebar.
func GetHistoryForFile(database *sql.DB, archivePath string) ([]HistoryEntry, error) {
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
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
