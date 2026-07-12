package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
)

// Stats holds aggregate information about the archive for the dashboard.
type Stats struct {
	TotalFiles      int64      `json:"total_files"`
	TotalSize       int64      `json:"total_size"`
	TotalSizeHuman  string     `json:"total_size_human"`
	TaggedFiles     int64      `json:"tagged_files"`
	Extensions      []ExtStat  `json:"extensions"`
	ProxyDone       int64      `json:"proxy_done"`
	ProxyPending    int64      `json:"proxy_pending"`    // pending + processing
	ProxyFailed     int64      `json:"proxy_failed"`
	ProxyConverting bool       `json:"proxy_converting"` // true if any row is 'processing'
}

// ExtStat is per-extension aggregate data.
type ExtStat struct {
	Extension string `json:"extension"`
	Count     int64  `json:"count"`
	Size      int64  `json:"size"`
	SizeHuman string `json:"size_human"`
}

// GetStats returns aggregate stats for the dashboard.
func GetStats(database *sql.DB) (*Stats, error) {
	stats := &Stats{}

	if err := database.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(size), 0) FROM file_registry WHERE trashed_at IS NULL`,
	).Scan(&stats.TotalFiles, &stats.TotalSize); err != nil {
		return nil, fmt.Errorf("stats query: %w", err)
	}
	stats.TotalSizeHuman = humanize.Bytes(uint64(stats.TotalSize))

	if err := database.QueryRow(
		`SELECT COUNT(DISTINCT file_id) FROM file_tags`,
	).Scan(&stats.TaggedFiles); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("tagged files query: %w", err)
	}

	rows, err := database.Query(`SELECT file_name, size FROM file_registry WHERE trashed_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("extension query: %w", err)
	}
	defer rows.Close()

	extMap := make(map[string]*ExtStat)
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			return nil, err
		}
		ext := extFromFilename(name)
		if extMap[ext] == nil {
			extMap[ext] = &ExtStat{Extension: ext}
		}
		extMap[ext].Count++
		extMap[ext].Size += size
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, es := range extMap {
		es.SizeHuman = humanize.Bytes(uint64(es.Size))
		stats.Extensions = append(stats.Extensions, *es)
	}
	sort.Slice(stats.Extensions, func(i, j int) bool {
		return stats.Extensions[i].Count > stats.Extensions[j].Count
	})
	if len(stats.Extensions) > 20 {
		stats.Extensions = stats.Extensions[:20]
	}

	// Proxy summary counts for the dashboard.
	proxyRows, err := database.Query(`
		SELECT proxy_status, COUNT(*)
		FROM   file_registry
		WHERE  trashed_at IS NULL AND proxy_status IS NOT NULL
		GROUP  BY proxy_status
	`)
	if err == nil {
		defer proxyRows.Close()
		for proxyRows.Next() {
			var status string
			var count int64
			if err := proxyRows.Scan(&status, &count); err == nil {
				switch status {
				case "done":
					stats.ProxyDone = count
				case "pending":
					stats.ProxyPending += count
				case "processing":
					stats.ProxyPending += count
					stats.ProxyConverting = true
				case "failed":
					stats.ProxyFailed = count
				}
			}
		}
	}

	return stats, nil
}

func extFromFilename(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		return "no_extension"
	}
	return ext
}
