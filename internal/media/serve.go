package media

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ServeFileContent streams a file from disk with full Range-request support,
// enabling video/audio seeking in the browser. The archive_path must reside
// under archiveRoot; a 403 is returned if it does not (path-traversal guard).
// Set forceDownload to add a Content-Disposition: attachment header.
func ServeFileContent(w http.ResponseWriter, r *http.Request, archivePath, archiveRoot string, forceDownload bool) {
	absPath, err := filepath.Abs(archivePath)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	absRoot, err := filepath.Abs(archiveRoot)
	if err != nil {
		http.Error(w, "invalid root", http.StatusInternalServerError)
		return
	}

	// Path-traversal guard: absPath must be inside absRoot.
	if absPath != absRoot && !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "cannot open file", http.StatusInternalServerError)
		}
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "stat error", http.StatusInternalServerError)
		return
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(absPath), "."))
	contentType := MIMEForExt(ext)

	w.Header().Set("Content-Type", contentType)
	// Allow PDF embedding in same-origin iframes.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")

	if forceDownload {
		name := filepath.Base(absPath)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}

	// http.ServeContent handles If-None-Match, Range, Content-Length, etc.
	http.ServeContent(w, r, filepath.Base(absPath), info.ModTime(), f)
}
