package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"filearchiver/internal/db"
	"github.com/go-chi/chi/v5"
)

// handleListTrash returns all files currently in the trash.
func handleListTrash(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		files, err := db.ListTrashedFiles(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if files == nil {
			files = []db.File{}
		}
		writeJSON(w, http.StatusOK, files)
	}
}

// handleRestoreTrash moves a trashed file back to its original archive location.
func handleRestoreTrash(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		file, err := db.GetFile(cfg.DB, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if file == nil || file.TrashedAt == nil {
			writeError(w, http.StatusNotFound, "trashed file not found")
			return
		}

		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		absSrc, _ := filepath.Abs(file.ArchivePath) // current trash path

		if !isUnderRoot(absSrc, absRoot) {
			writeError(w, http.StatusForbidden, "path outside archive root")
			return
		}

		// RestoreFileRecord updates the DB and returns the original archive path.
		restorePath, err := db.RestoreFileRecord(cfg.DB, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		absDst, _ := filepath.Abs(restorePath)
		if !isUnderRoot(absDst, absRoot) {
			// Roll back DB if path is suspect.
			_ = db.TrashFile(cfg.DB, id, file.ArchivePath)
			writeError(w, http.StatusForbidden, "restore path outside archive root")
			return
		}

		if err := os.MkdirAll(filepath.Dir(absDst), 0755); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot create restore directory: "+err.Error())
			return
		}

		if err := os.Rename(absSrc, absDst); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot restore file: "+err.Error())
			return
		}

		_ = db.LogFileAction(cfg.DB, &id, file.FileName, restorePath,
			"restored", "restored from _trash/ via web UI")

		updated, _ := db.GetFile(cfg.DB, id)
		writeJSON(w, http.StatusOK, updated)
	}
}

// handlePermanentlyDeleteTrash permanently removes a single trashed file from
// disk and its registry record. Requires confirm=true.
func handlePermanentlyDeleteTrash(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("confirm") != "true" {
			writeError(w, http.StatusBadRequest, "confirm=true is required")
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file id")
			return
		}

		file, err := db.GetFile(cfg.DB, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if file == nil || file.TrashedAt == nil {
			writeError(w, http.StatusNotFound, "trashed file not found")
			return
		}

		absPath, _ := filepath.Abs(file.ArchivePath)
		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		if !isUnderRoot(absPath, absRoot) {
			writeError(w, http.StatusForbidden, "path outside archive root")
			return
		}

		// Log before deleting the registry record (so file_id FK is still valid).
		_ = db.LogFileAction(cfg.DB, &id, file.FileName, file.ArchivePath,
			"permanently_deleted", "permanently deleted from _trash/ via web UI")

		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			writeError(w, http.StatusInternalServerError, "cannot delete file: "+err.Error())
			return
		}

		// Remove proxy file if one was generated.
		if file.ProxyPath != "" {
			_ = os.Remove(file.ProxyPath) // ignore error — proxy is best-effort
		}

		if err := db.DeleteFileRecord(cfg.DB, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"permanently_deleted": id})
	}
}

// handleEmptyTrash permanently deletes ALL trashed files. Requires confirm=true.
func handleEmptyTrash(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("confirm") != "true" {
			writeError(w, http.StatusBadRequest, "confirm=true is required")
			return
		}

		files, err := db.ListTrashedFiles(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		deleted := 0
		var errs []string

		for _, f := range files {
			absPath, _ := filepath.Abs(f.ArchivePath)
			if !isUnderRoot(absPath, absRoot) {
				continue
			}

			fID := f.ID
			_ = db.LogFileAction(cfg.DB, &fID, f.FileName, f.ArchivePath,
				"permanently_deleted", "permanently deleted via empty trash")

			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, f.FileName+": "+err.Error())
				continue
			}

			// Remove proxy file if one was generated.
			if f.ProxyPath != "" {
				_ = os.Remove(f.ProxyPath)
			}

			if err := db.DeleteFileRecord(cfg.DB, f.ID); err != nil {
				errs = append(errs, f.FileName+": "+err.Error())
				continue
			}
			deleted++
		}

		resp := map[string]any{"deleted": deleted}
		if len(errs) > 0 {
			resp["errors"] = errs
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleListFileActions returns the paginated file_actions audit log.
func handleListFileActions(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		page, _ := strconv.Atoi(q.Get("page"))
		perPage, _ := strconv.Atoi(q.Get("per_page"))

		result, err := db.ListAllFileActions(cfg.DB, db.FileActionListParams{
			Action:  q.Get("action"),
			Search:  q.Get("q"),
			From:    q.Get("from"),
			To:      q.Get("to"),
			Page:    page,
			PerPage: perPage,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
