package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"filearchiver/internal/db"
)

// handleListDuplicates returns all duplicate groups.
func handleListDuplicates(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups, err := db.GetDuplicateGroups(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if groups == nil {
			groups = []db.DuplicateGroup{}
		}
		writeJSON(w, http.StatusOK, groups)
	}
}

// handleDeleteFile moves a file to the _trash directory and marks it as trashed
// in the registry. A confirm=true query param is required as a safety guard.
func handleDeleteFile(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("confirm") != "true" {
			writeError(w, http.StatusBadRequest, "confirm=true is required")
			return
		}
		id, err := parseID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		file, err := db.GetFile(cfg.DB, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if file == nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}

		// Path-traversal guard before touching the filesystem.
		absPath, _ := filepath.Abs(file.ArchivePath)
		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		if !isUnderRoot(absPath, absRoot) {
			writeError(w, http.StatusForbidden, "path outside archive root")
			return
		}

		// Resolve a safe destination inside _trash/.
		trashDir := filepath.Join(absRoot, "_trash")
		if err := os.MkdirAll(trashDir, 0755); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot create trash directory: "+err.Error())
			return
		}
		trashPath, err := resolveTrashPath(trashDir, file.FileName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Move file into trash (tolerate already-missing files).
		if err := os.Rename(absPath, trashPath); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusInternalServerError, "cannot move file to trash: "+err.Error())
			return
		}

		if err := db.TrashFile(cfg.DB, id, trashPath); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		_ = db.LogFileAction(cfg.DB, &id, file.FileName, file.ArchivePath,
			"trashed", "moved to _trash/ via web UI")

		writeJSON(w, http.StatusOK, map[string]any{"trashed": id, "trash_path": trashPath})
	}
}

// handlePromoteDuplicate moves a duplicate file to its primary path, then
// removes the primary record from the registry. The body may contain:
//
//	{"primary_id": 123}
//
// If primary_id is omitted, the primary path is derived automatically and no
// primary registry record is deleted.
func handlePromoteDuplicate(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		var body struct {
			PrimaryID int64 `json:"primary_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // optional body

		file, err := db.GetFile(cfg.DB, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if file == nil {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		if !file.IsDuplicate {
			writeError(w, http.StatusBadRequest, "file is not a duplicate")
			return
		}

		targetPath := db.DerivePrimaryPath(file.ArchivePath)

		absTarget, _ := filepath.Abs(targetPath)
		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		if !isUnderRoot(absTarget, absRoot) {
			writeError(w, http.StatusForbidden, "derived path outside archive root")
			return
		}
		absSrc, _ := filepath.Abs(file.ArchivePath)
		if !isUnderRoot(absSrc, absRoot) {
			writeError(w, http.StatusForbidden, "source path outside archive root")
			return
		}

		// If a primary_id is provided, delete its file from disk.
		if body.PrimaryID > 0 {
			primary, err := db.GetFile(cfg.DB, body.PrimaryID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if primary != nil {
				absP, _ := filepath.Abs(primary.ArchivePath)
				if isUnderRoot(absP, absRoot) {
					if err := os.Remove(absP); err != nil && !os.IsNotExist(err) {
						writeError(w, http.StatusInternalServerError, "cannot delete primary: "+err.Error())
						return
					}
				}
			}
		}

		// Ensure target directory exists.
		if err := os.MkdirAll(filepath.Dir(absTarget), 0755); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot create target dir: "+err.Error())
			return
		}

		// Move duplicate → primary path.
		if err := os.Rename(absSrc, absTarget); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot move file: "+err.Error())
			return
		}

		// Update registry.
		if err := db.PromoteDuplicateRecord(cfg.DB, id, body.PrimaryID, targetPath); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		updated, _ := db.GetFile(cfg.DB, id)
		writeJSON(w, http.StatusOK, updated)
	}
}

// handleBulkDeleteIdentical deletes all duplicate files whose checksum
// matches the corresponding primary. Returns a count of deleted files.
func handleBulkDeleteIdentical(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("confirm") != "true" {
			writeError(w, http.StatusBadRequest, "confirm=true is required")
			return
		}

		groups, err := db.GetDuplicateGroups(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		deleted := 0
		var errs []string

		for _, g := range groups {
			if g.Primary == nil {
				continue // no primary to compare against
			}
			for _, dup := range g.Duplicates {
				if dup.Checksum == "" || dup.Checksum != g.Primary.Checksum {
					continue // not identical
				}
				absPath, _ := filepath.Abs(dup.ArchivePath)
				if !isUnderRoot(absPath, absRoot) {
					continue
				}
				if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
					errs = append(errs, "delete "+dup.FileName+": "+err.Error())
					continue
				}
				if err := db.DeleteFileRecord(cfg.DB, dup.ID); err != nil {
					errs = append(errs, "registry "+dup.FileName+": "+err.Error())
					continue
				}
				deleted++
			}
		}

		// Purge any cached thumbnails for deleted files (best-effort).
		if cfg.ThumbDir != "" {
			_ = purgeThumbs(cfg.ThumbDir)
		}

		resp := map[string]any{"deleted": deleted}
		if len(errs) > 0 {
			resp["errors"] = errs
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────────────────────────────────

// handleRescanDuplicates performs a full checksum-based re-scan of the archive:
//   - Orphaned _duplicates/ entries (unique checksum, no primary) → promoted to
//     their derived primary path.
//   - Multiple primary-path files with the same checksum → the extras are moved
//     into a _duplicates/ subdirectory alongside the canonical primary.
//
// Requires confirm=true query param as a safety guard.
func handleRescanDuplicates(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("confirm") != "true" {
			writeError(w, http.StatusBadRequest, "confirm=true is required")
			return
		}

		changes, err := db.PlanRescan(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "plan rescan: "+err.Error())
			return
		}

		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		result := db.RescanResult{}

		for _, c := range changes {
			absOld, _ := filepath.Abs(c.OldPath)
			absNew, _ := filepath.Abs(c.NewPath)

			if !isUnderRoot(absOld, absRoot) || !isUnderRoot(absNew, absRoot) {
				result.Errors = append(result.Errors, c.FileName+": path outside archive root")
				continue
			}

			// Ensure destination directory exists.
			if err := os.MkdirAll(filepath.Dir(absNew), 0755); err != nil {
				result.Errors = append(result.Errors, c.FileName+": mkdir: "+err.Error())
				continue
			}

			// Avoid overwriting an existing file at the destination.
			if _, statErr := os.Stat(absNew); statErr == nil {
				result.Errors = append(result.Errors,
					c.FileName+": destination already exists, skipping")
				continue
			}

			if err := os.Rename(absOld, absNew); err != nil {
				result.Errors = append(result.Errors, c.FileName+": rename: "+err.Error())
				continue
			}

			if err := db.UpdateArchivePath(cfg.DB, c.FileID, c.NewPath); err != nil {
				result.Errors = append(result.Errors, c.FileName+": db update: "+err.Error())
				continue
			}

			switch c.ChangeType {
			case "promote":
				result.Promoted++
			case "new_dup":
				result.NewDups++
			}
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func isUnderRoot(absPath, absRoot string) bool {
	return absPath == absRoot ||
		len(absPath) > len(absRoot) &&
			absPath[:len(absRoot)+1] == absRoot+string(filepath.Separator)
}

// purgeThumbs removes all .jpg files from the thumbnail cache directory.
func purgeThumbs(thumbDir string) error {
	matches, err := filepath.Glob(filepath.Join(thumbDir, "*.jpg"))
	if err != nil {
		return err
	}
	for _, m := range matches {
		_ = os.Remove(m)
	}
	return nil
}

// resolveTrashPath returns a unique destination path inside trashDir for the
// given filename. If the base name already exists it appends _01.._99.
func resolveTrashPath(trashDir, fileName string) (string, error) {
	base := filepath.Join(trashDir, fileName)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base, nil
	}
	ext := filepath.Ext(fileName)
	stem := strings.TrimSuffix(fileName, ext)
	for i := 1; i <= 99; i++ {
		candidate := filepath.Join(trashDir, fmt.Sprintf("%s_%02d%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("too many files named %q in trash", fileName)
}
