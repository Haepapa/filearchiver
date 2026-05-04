package db

import (
	"database/sql"
	"fmt"
	"sort"
)

// NavTypeEntry is a file extension with aggregate file count and total size.
type NavTypeEntry struct {
	Extension string `json:"extension"`
	Count     int64  `json:"count"`
	Size      int64  `json:"size"`
}

// NavYearEntry is a year node in the date-based navigation tree.
type NavYearEntry struct {
	Year   string          `json:"year"`
	Count  int64           `json:"count"`
	Months []NavMonthEntry `json:"months"`
}

// NavMonthEntry is a month node under a year in the date-based navigation tree.
type NavMonthEntry struct {
	Month string `json:"month"`
	Count int64  `json:"count"`
}

// NavTagCategory is a tag category with its child tags for the navigation tree.
type NavTagCategory struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Color string   `json:"color"`
	Tags  []NavTag `json:"tags"`
}

// NavTag is a single tag with its file count.
type NavTag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// GetNavTypes returns all extensions present in the archive, sorted by count desc.
func GetNavTypes(database *sql.DB) ([]NavTypeEntry, error) {
	rows, err := database.Query(`SELECT file_name, size FROM file_registry`)
	if err != nil {
		return nil, fmt.Errorf("nav types query: %w", err)
	}
	defer rows.Close()

	extMap := make(map[string]*NavTypeEntry)
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			return nil, err
		}
		ext := extFromFilename(name)
		if extMap[ext] == nil {
			extMap[ext] = &NavTypeEntry{Extension: ext}
		}
		extMap[ext].Count++
		extMap[ext].Size += size
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]NavTypeEntry, 0, len(extMap))
	for _, e := range extMap {
		result = append(result, *e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	return result, nil
}

// GetNavDates returns a year→month tree of file counts based on mod_time,
// ordered most-recent year first with months ascending within each year.
func GetNavDates(database *sql.DB) ([]NavYearEntry, error) {
	rows, err := database.Query(`
		SELECT
			SUBSTR(mod_time, 1, 4) AS yr,
			SUBSTR(mod_time, 6, 2) AS mo,
			COUNT(*)               AS cnt
		FROM file_registry
		WHERE mod_time IS NOT NULL AND mod_time != ''
		GROUP BY yr, mo
		ORDER BY yr DESC, mo ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("nav dates query: %w", err)
	}
	defer rows.Close()

	yearMap := make(map[string]*NavYearEntry)
	var yearOrder []string

	for rows.Next() {
		var yr, mo string
		var cnt int64
		if err := rows.Scan(&yr, &mo, &cnt); err != nil {
			return nil, err
		}
		if _, ok := yearMap[yr]; !ok {
			yearMap[yr] = &NavYearEntry{Year: yr}
			yearOrder = append(yearOrder, yr)
		}
		yearMap[yr].Count += cnt
		yearMap[yr].Months = append(yearMap[yr].Months, NavMonthEntry{Month: mo, Count: cnt})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]NavYearEntry, 0, len(yearOrder))
	for _, yr := range yearOrder {
		result = append(result, *yearMap[yr])
	}
	return result, nil
}

// GetNavTags returns all tag categories with their child tags and file counts,
// sorted alphabetically by category and tag name.
func GetNavTags(database *sql.DB) ([]NavTagCategory, error) {
	rows, err := database.Query(`
		SELECT
			tc.id,   tc.name,  tc.color,
			t.id,    t.name,
			COUNT(ft.file_id) AS file_count
		FROM tag_categories tc
		LEFT JOIN tags       t  ON t.category_id = tc.id
		LEFT JOIN file_tags  ft ON ft.tag_id      = t.id
		GROUP BY tc.id, tc.name, tc.color, t.id, t.name
		ORDER BY tc.name, t.name
	`)
	if err != nil {
		return nil, fmt.Errorf("nav tags query: %w", err)
	}
	defer rows.Close()

	catMap := make(map[int64]*NavTagCategory)
	var catOrder []int64

	for rows.Next() {
		var catID int64
		var catName, catColor string
		var tagID sql.NullInt64
		var tagName sql.NullString
		var fileCount int64

		if err := rows.Scan(&catID, &catName, &catColor, &tagID, &tagName, &fileCount); err != nil {
			return nil, err
		}
		if _, ok := catMap[catID]; !ok {
			catMap[catID] = &NavTagCategory{ID: catID, Name: catName, Color: catColor, Tags: []NavTag{}}
			catOrder = append(catOrder, catID)
		}
		if tagID.Valid && tagName.Valid {
			catMap[catID].Tags = append(catMap[catID].Tags, NavTag{
				ID:    tagID.Int64,
				Name:  tagName.String,
				Count: fileCount,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]NavTagCategory, 0, len(catOrder))
	for _, id := range catOrder {
		result = append(result, *catMap[id])
	}
	return result, nil
}

// GetRecentHistory returns the most recent successful archive operations,
// capped at limit entries (use 0 for the default of 20).
func GetRecentHistory(database *sql.DB, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := database.Query(`
		SELECT id, timestamp, job_name, status, message
		FROM history
		WHERE status = 'SUCCESS'
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent history query: %w", err)
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
