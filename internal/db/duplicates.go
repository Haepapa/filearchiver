package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

// DuplicateGroup pairs a primary archived file with one or more duplicate
// files that share the same content (by checksum) or the same file_name under
// a `_duplicates/` path when no checksum is available.
type DuplicateGroup struct {
	FileName   string  `json:"file_name"`
	Checksum   string  `json:"checksum,omitempty"`
	Primary    *File   `json:"primary"`    // nil when the primary has been deleted/moved
	Duplicates []*File `json:"duplicates"` // always ≥ 1
}

// RescanResult summarises the changes made during a checksum re-scan.
type RescanResult struct {
	Promoted int      `json:"promoted"`  // orphaned _duplicates/ files promoted to primary path
	NewDups  int      `json:"new_dups"`  // previously-unlabelled files moved into _duplicates/
	Errors   []string `json:"errors,omitempty"`
}

// RescanChange describes a single change the rescan wants to make.
type RescanChange struct {
	FileID      int64
	OldPath     string
	NewPath     string
	FileName    string
	ChangeType  string // "promote" | "new_dup"
}


// GetDuplicateGroups returns all duplicate groups using checksum-first grouping.
// Files with a non-empty checksum are grouped by content so cross-name duplicates
// are surfaced. Files without a checksum fall back to file_name grouping.
// Each group contains the non-duplicate primary (if present) and all
// _duplicates counterparts.
func GetDuplicateGroups(database *sql.DB) ([]DuplicateGroup, error) {
	rows, err := database.Query(`
		SELECT id, original_path, archive_path, file_name, size,
		       COALESCE(checksum,'') AS checksum, mod_time,
		       COALESCE(proxy_path,'') AS proxy_path,
		       COALESCE(proxy_status,'') AS proxy_status
		FROM file_registry
		WHERE trashed_at IS NULL
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("duplicate groups query: %w", err)
	}
	defer rows.Close()

	type fileRow struct {
		file File
	}

	var allFiles []File
	for rows.Next() {
		var f File
		var modTimeStr string
		if err := rows.Scan(
			&f.ID, &f.OriginalPath, &f.ArchivePath,
			&f.FileName, &f.Size, &f.Checksum, &modTimeStr,
			&f.ProxyPath, &f.ProxyStatus,
		); err != nil {
			return nil, err
		}
		f.ModTime = parseTime(modTimeStr)
		f.Extension = fileExtension(f.FileName)
		f.IsDuplicate = isDuplicate(f.ArchivePath)
		allFiles = append(allFiles, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// ── Group by checksum (content-first) ────────────────────────────────────
	byChecksum := make(map[string][]File)
	var checksumOrder []string
	seenChecksum := make(map[string]bool)

	var noChecksumFiles []File // fall back to name-based grouping

	for _, f := range allFiles {
		if f.Checksum == "" {
			noChecksumFiles = append(noChecksumFiles, f)
			continue
		}
		if !seenChecksum[f.Checksum] {
			seenChecksum[f.Checksum] = true
			checksumOrder = append(checksumOrder, f.Checksum)
		}
		byChecksum[f.Checksum] = append(byChecksum[f.Checksum], f)
	}

	var groups []DuplicateGroup

	// Checksum-based groups.
	for _, ck := range checksumOrder {
		files := byChecksum[ck]

		var primary *File
		var dups []*File
		for i := range files {
			f := &files[i]
			if f.IsDuplicate {
				cp := *f
				dups = append(dups, &cp)
			} else if primary == nil {
				cp := *f
				primary = &cp
			}
		}
		if len(dups) == 0 {
			continue // no duplicate entries for this checksum
		}

		name := ""
		if primary != nil {
			name = primary.FileName
		} else if len(dups) > 0 {
			name = dups[0].FileName
		}

		groups = append(groups, DuplicateGroup{
			FileName:   name,
			Checksum:   ck,
			Primary:    primary,
			Duplicates: dups,
		})
	}

	// Fall-back: file_name grouping for files without checksums.
	byName := make(map[string][]File)
	var nameOrder []string
	seenName := make(map[string]bool)
	for _, f := range noChecksumFiles {
		if !seenName[f.FileName] {
			seenName[f.FileName] = true
			nameOrder = append(nameOrder, f.FileName)
		}
		byName[f.FileName] = append(byName[f.FileName], f)
	}
	for _, name := range nameOrder {
		files := byName[name]
		var primary *File
		var dups []*File
		for i := range files {
			f := &files[i]
			if f.IsDuplicate {
				cp := *f
				dups = append(dups, &cp)
			} else if primary == nil {
				cp := *f
				primary = &cp
			}
		}
		if len(dups) == 0 {
			continue
		}
		groups = append(groups, DuplicateGroup{
			FileName:   name,
			Primary:    primary,
			Duplicates: dups,
		})
	}

	return groups, nil
}

// PlanRescan analyses the file registry by checksum and returns the list of
// changes needed to normalise duplicate state:
//
//   - "promote": a file currently in _duplicates/ is the only copy → move to
//     its derived primary path.
//   - "new_dup": two or more primary-path files share a checksum → all but the
//     first (lowest ID) should move into a _duplicates/ sub-directory.
func PlanRescan(database *sql.DB) ([]RescanChange, error) {
	rows, err := database.Query(`
		SELECT id, archive_path, file_name, COALESCE(checksum,'') AS checksum
		FROM file_registry
		WHERE trashed_at IS NULL AND checksum != ''
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("rescan query: %w", err)
	}
	defer rows.Close()

	type entry struct {
		ID          int64
		ArchivePath string
		FileName    string
		Checksum    string
		IsDup       bool
	}

	byChecksum := make(map[string][]entry)
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.ArchivePath, &e.FileName, &e.Checksum); err != nil {
			return nil, err
		}
		e.IsDup = isDuplicate(e.ArchivePath)
		byChecksum[e.Checksum] = append(byChecksum[e.Checksum], e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var changes []RescanChange

	for _, files := range byChecksum {
		var primaries, dups []entry
		for _, f := range files {
			if f.IsDup {
				dups = append(dups, f)
			} else {
				primaries = append(primaries, f)
			}
		}

		switch {
		case len(primaries) == 0 && len(dups) == 1:
			// Orphaned duplicate — unique content, no primary: promote.
			d := dups[0]
			newPath := DerivePrimaryPath(d.ArchivePath)
			changes = append(changes, RescanChange{
				FileID: d.ID, OldPath: d.ArchivePath, NewPath: newPath,
				FileName: d.FileName, ChangeType: "promote",
			})

		case len(primaries) > 1:
			// Multiple primaries with same checksum — keep first, move rest to _dups.
			canonical := primaries[0]
			canonicalDir := filepath.Dir(canonical.ArchivePath)
			for _, extra := range primaries[1:] {
				dupDir := filepath.Join(canonicalDir, "_duplicates")
				newPath := filepath.Join(dupDir, extra.FileName)
				changes = append(changes, RescanChange{
					FileID: extra.ID, OldPath: extra.ArchivePath, NewPath: newPath,
					FileName: extra.FileName, ChangeType: "new_dup",
				})
			}
		}
	}
	return changes, nil
}

// UpdateArchivePath updates the archive_path for a single file registry record.
func UpdateArchivePath(database *sql.DB, id int64, newPath string) error {
	_, err := database.Exec(
		`UPDATE file_registry SET archive_path = ? WHERE id = ?`, newPath, id,
	)
	if err != nil {
		return fmt.Errorf("update archive path id=%d: %w", id, err)
	}
	return nil
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
