package api

import (
	"encoding/json"
	"net/http"
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
