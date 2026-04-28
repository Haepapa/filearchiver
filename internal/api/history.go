package api

import (
	"net/http"
	"strconv"

	"filearchiver/internal/db"
)

func handleListHistory(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		page, _ := strconv.Atoi(q.Get("page"))
		perPage, _ := strconv.Atoi(q.Get("per_page"))

		params := db.HistoryListParams{
			JobName:       q.Get("job"),
			Status:        q.Get("status"),
			From:          q.Get("from"),
			To:            q.Get("to"),
			MessageSearch: q.Get("message"),
			Page:          page,
			PerPage:       perPage,
		}

		result, err := db.ListHistory(cfg.DB, params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetRecentHistory(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := db.GetRecentHistory(cfg.DB, 20)
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
