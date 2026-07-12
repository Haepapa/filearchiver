package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"filearchiver/internal/db"
)

func handleListFiles(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		page, _ := strconv.Atoi(q.Get("page"))
		perPage, _ := strconv.Atoi(q.Get("per_page"))

		params := db.FileListParams{
			Query:          q.Get("q"),
			Extension:      q.Get("ext"),
			TagName:        q.Get("tag"),
			From:           q.Get("from"),
			To:             q.Get("to"),
			Year:           q.Get("year"),
			Month:          q.Get("month"),
			DuplicatesOnly: q.Get("duplicates_only") == "true" || q.Get("duplicates_only") == "1",
			Page:           page,
			PerPage:        perPage,
			Sort:           q.Get("sort"),
			Order:          q.Get("order"),
		}

		result, err := db.ListFiles(cfg.DB, params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetFile(cfg Config) http.HandlerFunc {
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

		writeJSON(w, http.StatusOK, file)
	}
}

// handleBulkTrashFiles moves multiple files to trash in one request.
// Body: {"ids": [1, 2, 3], "confirm": true}
func handleBulkTrashFiles(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs     []int64 `json:"ids"`
			Confirm bool    `json:"confirm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if !body.Confirm {
			writeError(w, http.StatusBadRequest, "confirm must be true")
			return
		}
		if len(body.IDs) == 0 {
			writeError(w, http.StatusBadRequest, "no file ids provided")
			return
		}
		if len(body.IDs) > 500 {
			writeError(w, http.StatusBadRequest, "too many ids (max 500)")
			return
		}

		absRoot, _ := filepath.Abs(cfg.ArchiveRoot)
		trashDir := filepath.Join(absRoot, "_trash")
		if err := os.MkdirAll(trashDir, 0755); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot create trash directory: "+err.Error())
			return
		}

		trashed := 0
		var errs []string

		for _, id := range body.IDs {
			file, err := db.GetFile(cfg.DB, id)
			if err != nil || file == nil {
				errs = append(errs, fmt.Sprintf("id %d: not found", id))
				continue
			}

			absPath, _ := filepath.Abs(file.ArchivePath)
			if !isUnderRoot(absPath, absRoot) {
				errs = append(errs, fmt.Sprintf("%s: path outside archive root", file.FileName))
				continue
			}

			trashPath, err := resolveTrashPath(trashDir, file.FileName)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %s", file.FileName, err.Error()))
				continue
			}

			if err := os.Rename(absPath, trashPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Sprintf("%s: %s", file.FileName, err.Error()))
				continue
			}

			if err := db.TrashFile(cfg.DB, id, trashPath); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %s", file.FileName, err.Error()))
				continue
			}

			fID := id
			_ = db.LogFileAction(cfg.DB, &fID, file.FileName, file.ArchivePath,
				"trashed", "bulk move to trash via web UI")

			trashed++
		}

		resp := map[string]any{"trashed": trashed}
		if len(errs) > 0 {
			resp["errors"] = errs
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleRegenerateProxy resets a file's proxy status to 'pending' and deletes
// any existing proxy file on disk so the worker will regenerate it.
func handleRegenerateProxy(cfg Config) http.HandlerFunc {
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

		// Delete the existing proxy file from disk if present.
		if file.ProxyPath != "" {
			if removeErr := os.Remove(file.ProxyPath); removeErr != nil && !os.IsNotExist(removeErr) {
				writeError(w, http.StatusInternalServerError, "failed to remove proxy file: "+removeErr.Error())
				return
			}
		}

		// Reset proxy status to pending so the worker picks it up.
		if err := db.ResetFileProxy(cfg.DB, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
	}
}

// writeJSON serialises v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encoding error", http.StatusInternalServerError)
	}
}

// writeError writes a JSON error body.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
