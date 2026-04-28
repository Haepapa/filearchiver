package media_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"filearchiver/internal/media"
)

func writeTempFile(t *testing.T, name, content string) (path string, root string) {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "archive")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sub, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p, dir
}

func TestServeFileContent_OK(t *testing.T) {
	path, root := writeTempFile(t, "hello.txt", "hello world")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	media.ServeFileContent(rw, req, path, root, false)

	res := rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if ct == "" {
		t.Error("Content-Type header missing")
	}
}

func TestServeFileContent_Download(t *testing.T) {
	path, root := writeTempFile(t, "report.pdf", "%PDF-1.4 fake")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	media.ServeFileContent(rw, req, path, root, true)

	res := rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	cd := res.Header.Get("Content-Disposition")
	if cd == "" {
		t.Error("Content-Disposition header missing for forceDownload=true")
	}
}

func TestServeFileContent_NotFound(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nonexistent.txt")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	media.ServeFileContent(rw, req, path, root, false)

	res := rw.Result()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}

func TestServeFileContent_PathTraversal(t *testing.T) {
	root := t.TempDir()
	// Try to escape the archive root.
	evilPath := filepath.Join(root, "..", "secret.txt")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	media.ServeFileContent(rw, req, evilPath, root, false)

	res := rw.Result()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("path traversal: status = %d, want 403", res.StatusCode)
	}
}
