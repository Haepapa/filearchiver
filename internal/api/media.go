package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"filearchiver/internal/db"
	"filearchiver/internal/media"
)

// handleFileContent streams the archived file to the browser with full Range
// support (enabling video/audio seeking). A ?download=true query parameter
// forces a Content-Disposition: attachment response.
func handleFileContent(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseID(r)
		if err != nil {
			http.Error(w, "invalid file id", http.StatusBadRequest)
			return
		}

		file, err := db.GetFile(cfg.DB, id)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		if file == nil {
			http.NotFound(w, r)
			return
		}

		forceDownload := r.URL.Query().Get("download") == "true"
		media.ServeFileContent(w, r, file.ArchivePath, cfg.ArchiveRoot, forceDownload)
	}
}

// handleFileThumbnail serves a cached JPEG thumbnail for image files.
// For non-thumbnailable files it returns 404. If thumbnail generation fails
// (e.g. the source file is not yet on disk) it falls back to the full image.
func handleFileThumbnail(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseID(r)
		if err != nil {
			http.Error(w, "invalid file id", http.StatusBadRequest)
			return
		}

		file, err := db.GetFile(cfg.DB, id)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		if file == nil {
			http.NotFound(w, r)
			return
		}

		if !media.IsThumbnailable(file.Extension) {
			http.NotFound(w, r)
			return
		}

		thumbPath, err := media.GenerateThumbnail(file.ArchivePath, cfg.ThumbDir)
		if err != nil {
			// Fall back to serving the original image so the grid still shows something.
			media.ServeFileContent(w, r, file.ArchivePath, cfg.ArchiveRoot, false)
			return
		}

		http.ServeFile(w, r, thumbPath)
	}
}

// handleGetFileHistory returns history log entries that reference a file's
// archive path, providing the audit trail shown in the viewer sidebar.
func handleGetFileHistory(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		entries, err := db.GetHistoryForFile(cfg.DB, file.ArchivePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if entries == nil {
			entries = []db.HistoryEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

// parseID extracts and validates the {id} URL parameter as int64.
func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
