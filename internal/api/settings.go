package api

import "net/http"

// ServerSettings is the payload returned by GET /api/settings.
type ServerSettings struct {
	DBPath      string `json:"db_path"`
	ArchiveRoot string `json:"archive_root"`
	ThumbDir    string `json:"thumb_dir"`
	Readonly    bool   `json:"readonly"`
}

func handleGetSettings(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ServerSettings{
			DBPath:      cfg.DBPath,
			ArchiveRoot: cfg.ArchiveRoot,
			ThumbDir:    cfg.ThumbDir,
			Readonly:    cfg.Readonly,
		})
	}
}
