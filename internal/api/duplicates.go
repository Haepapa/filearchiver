package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

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

// handleDeleteFile deletes a file from disk and removes its registry record.
// A confirm=true query param is required as a double-submit CSRF guard.
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

		// Delete from disk — tolerate already-missing files.
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusInternalServerError, "cannot delete file: "+err.Error())
			return
		}

		if err := db.DeleteFileRecord(cfg.DB, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
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
