package api

import (
	"net/http"

	"filearchiver/internal/db"
)

func handleGetNavTypes(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		types, err := db.GetNavTypes(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if types == nil {
			types = []db.NavTypeEntry{}
		}
		writeJSON(w, http.StatusOK, types)
	}
}

func handleGetNavDates(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dates, err := db.GetNavDates(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if dates == nil {
			dates = []db.NavYearEntry{}
		}
		writeJSON(w, http.StatusOK, dates)
	}
}

func handleGetNavTags(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tags, err := db.GetNavTags(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tags == nil {
			tags = []db.NavTagCategory{}
		}
		writeJSON(w, http.StatusOK, tags)
	}
}
