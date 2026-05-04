package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"filearchiver/internal/db"
)

// ──────────────────────────────────────────────────────────────────────────────
// Tag categories
// ──────────────────────────────────────────────────────────────────────────────

func handleListTagCategories(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cats, err := db.ListTagCategories(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if cats == nil {
			cats = []db.TagCategory{}
		}
		writeJSON(w, http.StatusOK, cats)
	}
}

func handleCreateTagCategory(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		cat, err := db.CreateTagCategory(cfg.DB, body.Name, body.Color)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, cat)
	}
}

func handleUpdateTagCategory(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseCatID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var body struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := db.UpdateTagCategory(cfg.DB, id, body.Name, body.Color); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteTagCategory(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseCatID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := db.DeleteTagCategory(cfg.DB, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Tags
// ──────────────────────────────────────────────────────────────────────────────

func handleListTags(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var catID int64
		if s := r.URL.Query().Get("category_id"); s != "" {
			catID, _ = strconv.ParseInt(s, 10, 64)
		}
		tags, err := db.ListTags(cfg.DB, catID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tags == nil {
			tags = []db.Tag{}
		}
		writeJSON(w, http.StatusOK, tags)
	}
}

func handleCreateTag(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name       string `json:"name"`
			CategoryID int64  `json:"category_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		tag, err := db.CreateTag(cfg.DB, body.Name, body.CategoryID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, tag)
	}
}

func handleUpdateTag(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseTagID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var body struct {
			Name       string `json:"name"`
			CategoryID *int64 `json:"category_id"` // pointer: null means "set to uncategorised"
			ChangeCat  bool   `json:"change_category"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		catID := int64(0)
		changeCat := body.ChangeCat
		if body.CategoryID != nil {
			catID = *body.CategoryID
			changeCat = true
		}
		if err := db.UpdateTag(cfg.DB, id, body.Name, catID, changeCat); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteTag(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseTagID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := db.DeleteTag(cfg.DB, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMergeTag(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceID, err := parseTagID(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid source id")
			return
		}
		var body struct {
			IntoID int64 `json:"into_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.IntoID == 0 {
			writeError(w, http.StatusBadRequest, "into_id is required")
			return
		}
		if sourceID == body.IntoID {
			writeError(w, http.StatusBadRequest, "source and target tags must differ")
			return
		}
		if err := db.MergeTags(cfg.DB, sourceID, body.IntoID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// File-tag associations
// ──────────────────────────────────────────────────────────────────────────────

func handleGetFileTags(cfg Config) http.HandlerFunc {
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
		tags, err := db.GetFileTags(cfg.DB, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tags)
	}
}

func handleSetFileTags(cfg Config) http.HandlerFunc {
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
		var body struct {
			TagIDs []int64 `json:"tag_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.TagIDs == nil {
			body.TagIDs = []int64{}
		}
		if err := db.SetFileTags(cfg.DB, id, body.TagIDs); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Return the updated tag list.
		tags, err := db.GetFileTags(cfg.DB, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tags)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// URL parameter helpers
// ──────────────────────────────────────────────────────────────────────────────

func parseCatID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "catID"), 10, 64)
}

func parseTagID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "tagID"), 10, 64)
}
