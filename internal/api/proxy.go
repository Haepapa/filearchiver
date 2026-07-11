package api

import (
	"encoding/json"
	"net/http"
	"os"

	"filearchiver/internal/db"
)

// handleProxyStatus returns the current proxy worker state and queue stats.
func handleProxyStatus(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.ProxyWorker == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"running": false,
				"paused":  false,
				"status":  "stopped",
				"stats":   db.ProxyStats{},
			})
			return
		}
		writeJSON(w, http.StatusOK, cfg.ProxyWorker.Status())
	}
}

// handleProxyPause pauses the proxy worker.
func handleProxyPause(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.ProxyWorker == nil {
			writeError(w, http.StatusServiceUnavailable, "proxy worker not running")
			return
		}
		cfg.ProxyWorker.Pause()
		writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
	}
}

// handleProxyResume resumes a paused proxy worker.
func handleProxyResume(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.ProxyWorker == nil {
			writeError(w, http.StatusServiceUnavailable, "proxy worker not running")
			return
		}
		cfg.ProxyWorker.Resume()
		writeJSON(w, http.StatusOK, map[string]string{"status": "running"})
	}
}

// handleProxyRestart resets failed proxies to pending and resumes the worker.
func handleProxyRestart(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.ProxyWorker == nil {
			writeError(w, http.StatusServiceUnavailable, "proxy worker not running")
			return
		}
		cfg.ProxyWorker.Restart()
		writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
	}
}

// handleGetProxySettings returns all proxy settings as a key/value map.
func handleGetProxySettings(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := db.GetAllProxySettings(cfg.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}
}

// handleSetProxySettings updates one or more proxy settings and reconfigures
// the worker if it is running.
func handleSetProxySettings(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var updates map[string]string
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Only allow known keys.
		allowed := map[string]bool{
			"enabled": true, "paused": true, "min_file_size_mb": true,
			"max_workers": true, "image_max_width": true, "image_quality": true,
			"video_max_width": true, "video_crf": true, "use_gpu": true,
		}
		for k := range updates {
			if !allowed[k] {
				writeError(w, http.StatusBadRequest, "unknown setting: "+k)
				return
			}
		}

		if err := db.SetProxySettings(cfg.DB, updates); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if cfg.ProxyWorker != nil {
			cfg.ProxyWorker.Reconfigure()
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleFileProxy serves the proxy file for a given file ID.
// Returns 404 if no proxy has been generated yet.
func handleFileProxy(cfg Config) http.HandlerFunc {
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
		if file.ProxyPath == "" || file.ProxyStatus != "done" {
			http.NotFound(w, r)
			return
		}

		// Ensure the proxy file actually exists on disk.
		if _, err := os.Stat(file.ProxyPath); err != nil {
			http.NotFound(w, r)
			return
		}

		http.ServeFile(w, r, file.ProxyPath)
	}
}
