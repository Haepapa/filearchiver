package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

// DuplicateGroup pairs a primary archived file with one or more duplicate
// files that live under a `_duplicates/` sub-path with the same base name.
type DuplicateGroup struct {
	FileName   string  `json:"file_name"`
	Primary    *File   `json:"primary"`    // nil when the primary has been deleted
	Duplicates []*File `json:"duplicates"` // always ≥ 1
}

// GetDuplicateGroups returns all duplicate groups.
// Each group contains the non-duplicate primary (if it exists) and all
// _duplicates counterparts matched by file_name.
func GetDuplicateGroups(database *sql.DB) ([]DuplicateGroup, error) {
	rows, err := database.Query(`
		SELECT id, original_path, archive_path, file_name, size, checksum, mod_time
		FROM file_registry
		ORDER BY file_name, archive_path
	`)
	if err != nil {
		return nil, fmt.Errorf("duplicate groups query: %w", err)
	}
	defer rows.Close()

	type fileRow struct {
		file  File
		isDup bool
	}

	// Collect all files, bucketed by file_name.
	byName := make(map[string][]fileRow)
	var nameOrder []string
	seen := make(map[string]bool)

	for rows.Next() {
		var f File
		var modTimeStr string
		if err := rows.Scan(
			&f.ID, &f.OriginalPath, &f.ArchivePath,
			&f.FileName, &f.Size, &f.Checksum, &modTimeStr,
		); err != nil {
			return nil, err
		}
		f.ModTime = parseTime(modTimeStr)
		f.Extension = fileExtension(f.FileName)
		f.IsDuplicate = isDuplicate(f.ArchivePath)

		if !seen[f.FileName] {
			seen[f.FileName] = true
			nameOrder = append(nameOrder, f.FileName)
		}
		byName[f.FileName] = append(byName[f.FileName], fileRow{file: f, isDup: f.IsDuplicate})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Build groups: only include names that have at least one duplicate.
	var groups []DuplicateGroup
	for _, name := range nameOrder {
		rows := byName[name]
		var primary *File
		var dups []*File

		for i := range rows {
			r := &rows[i]
			if r.isDup {
				cp := r.file
				dups = append(dups, &cp)
			} else if primary == nil {
				cp := r.file
				primary = &cp
			}
		}

		if len(dups) == 0 {
			continue // no duplicate entries for this name
		}
		groups = append(groups, DuplicateGroup{
			FileName:   name,
			Primary:    primary,
			Duplicates: dups,
		})
	}
	return groups, nil
}

// DeleteFileRecord removes a file from the file_registry table.
// Actual disk deletion must be performed by the caller before this call.
func DeleteFileRecord(database *sql.DB, id int64) error {
	res, err := database.Exec(`DELETE FROM file_registry WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete file record: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("file id %d not found", id)
	}
	return nil
}

// PromoteDuplicateRecord updates a duplicate file's archive_path to targetPath
// and (if primaryID > 0) deletes the primary's registry record.
// Disk operations must be handled by the caller.
func PromoteDuplicateRecord(database *sql.DB, dupID, primaryID int64, targetPath string) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`UPDATE file_registry SET archive_path = ? WHERE id = ?`, targetPath, dupID,
	); err != nil {
		return fmt.Errorf("update duplicate path: %w", err)
	}

	if primaryID > 0 {
		if _, err := tx.Exec(
			`DELETE FROM file_registry WHERE id = ?`, primaryID,
		); err != nil {
			return fmt.Errorf("delete primary record: %w", err)
		}
	}
	return tx.Commit()
}

// derivePrimaryPath converts a `_duplicates/…` archive path to the
// corresponding primary path by removing the `/_duplicates` segment.
func DerivePrimaryPath(dupPath string) string {
	slashed := filepath.ToSlash(dupPath)
	cleaned := strings.Replace(slashed, "/_duplicates", "", 1)
	return filepath.FromSlash(cleaned)
}
