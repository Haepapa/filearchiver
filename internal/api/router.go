package api

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"filearchiver/internal/webui"
)

// Config holds the dependencies injected into all API handlers.
type Config struct {
	DB          *sql.DB
	DBPath      string
	ArchiveRoot string
	Readonly    bool
	ThumbDir    string
}

// NewRouter builds and returns the chi router for the web UI server.
func NewRouter(cfg Config) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)

	// Media endpoints — registered BEFORE the JSON /api subrouter so they are
	// not wrapped by the jsonContentType middleware (they stream binary content).
	r.Get("/api/files/{id}/content",   handleFileContent(cfg))
	r.Get("/api/files/{id}/thumbnail", handleFileThumbnail(cfg))

	// JSON API routes
	r.Route("/api", func(r chi.Router) {
		r.Use(jsonContentType)

		r.Get("/stats", handleGetStats(cfg))
		r.Get("/settings", handleGetSettings(cfg))

		r.Route("/files", func(r chi.Router) {
			r.Get("/", handleListFiles(cfg))
			r.Get("/{id}", handleGetFile(cfg))
			r.Delete("/{id}", readonlyGuard(cfg, handleDeleteFile(cfg)))
			r.Get("/{id}/history", handleGetFileHistory(cfg))
			r.Get("/{id}/tags", handleGetFileTags(cfg))
			r.Put("/{id}/tags", readonlyGuard(cfg, handleSetFileTags(cfg)))
		})

		r.Route("/duplicates", func(r chi.Router) {
			r.Get("/", handleListDuplicates(cfg))
			r.Post("/bulk-delete-identical", readonlyGuard(cfg, handleBulkDeleteIdentical(cfg)))
			r.Post("/{id}/promote", readonlyGuard(cfg, handlePromoteDuplicate(cfg)))
		})

		r.Route("/history", func(r chi.Router) {
			r.Get("/", handleListHistory(cfg))
			r.Get("/recent", handleGetRecentHistory(cfg))
		})

		r.Route("/nav", func(r chi.Router) {
			r.Get("/types", handleGetNavTypes(cfg))
			r.Get("/dates", handleGetNavDates(cfg))
			r.Get("/tags", handleGetNavTags(cfg))
		})

		r.Route("/tag-categories", func(r chi.Router) {
			r.Get("/", handleListTagCategories(cfg))
			r.Post("/", readonlyGuard(cfg, handleCreateTagCategory(cfg)))
			r.Patch("/{catID}", readonlyGuard(cfg, handleUpdateTagCategory(cfg)))
			r.Delete("/{catID}", readonlyGuard(cfg, handleDeleteTagCategory(cfg)))
		})

		r.Route("/tags", func(r chi.Router) {
			r.Get("/", handleListTags(cfg))
			r.Post("/", readonlyGuard(cfg, handleCreateTag(cfg)))
			r.Patch("/{tagID}", readonlyGuard(cfg, handleUpdateTag(cfg)))
			r.Delete("/{tagID}", readonlyGuard(cfg, handleDeleteTag(cfg)))
			r.Post("/{tagID}/merge", readonlyGuard(cfg, handleMergeTag(cfg)))
		})
	})

	// Serve the embedded frontend for all other routes (SPA fallback).
	staticHandler := http.FileServer(http.FS(webui.SubFS()))
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		staticHandler.ServeHTTP(w, r)
	})

	return r
}

// jsonContentType sets Content-Type: application/json on all API responses.
func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// readonlyGuard returns 403 Forbidden when the server is in read-only mode.
func readonlyGuard(cfg Config, next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Readonly {
			writeError(w, http.StatusForbidden, "server is in read-only mode")
			return
		}
		next.ServeHTTP(w, r)
	}
}
