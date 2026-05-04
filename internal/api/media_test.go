package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"filearchiver/internal/api"
	"filearchiver/internal/db"
)

// setupTestServerWithFiles creates a test server with actual archive files on
// disk, enabling content and thumbnail endpoint tests.
func setupTestServerWithFiles(t *testing.T) (*httptest.Server, string, int64) {
	t.Helper()

	archiveRoot := t.TempDir()
	archiveDir  := filepath.Join(archiveRoot, "archive", "jpg", "2024", "01", "15")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(archiveDir, "test.txt")
	if err := os.WriteFile(archivePath, []byte("file content for test"), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.CreateTemp("", "famedia-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	database, err := db.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			job_name TEXT, status TEXT, message TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS file_registry (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			original_path TEXT, archive_path TEXT, file_name TEXT,
			size INTEGER, checksum TEXT, mod_time DATETIME
		)`,
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	res, err := database.Exec(
		`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"/src/test.txt", archivePath, "test.txt", 21, "abc123",
		time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	// Seed a history entry referencing this file.
	database.Exec(
		`INSERT INTO history (job_name, status, message) VALUES (?, ?, ?)`,
		"test-job", "success", "archived "+archivePath,
	)

	cfg := api.Config{
		DB:          database,
		ArchiveRoot: archiveRoot,
		Readonly:    false,
		ThumbDir:    t.TempDir(),
	}

	srv := httptest.NewServer(api.NewRouter(cfg))
	t.Cleanup(func() {
		srv.Close()
		database.Close()
		os.Remove(f.Name())
	})
	return srv, archivePath, id
}

func TestFileContentEndpoint(t *testing.T) {
	srv, _, id := setupTestServerWithFiles(t)
	url := srv.URL + "/api/files/" + itoa(id) + "/content"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		t.Error("Content-Type missing")
	}
}

func TestFileContentEndpoint_Download(t *testing.T) {
	srv, _, id := setupTestServerWithFiles(t)
	url := srv.URL + "/api/files/" + itoa(id) + "/content?download=true"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if cd == "" {
		t.Error("Content-Disposition missing for download=true")
	}
}

func TestFileContentEndpoint_NotFound(t *testing.T) {
	srv, _, _ := setupTestServerWithFiles(t)
	url := srv.URL + "/api/files/99999/content"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestFileThumbnailEndpoint_NonThumbable(t *testing.T) {
	srv, _, id := setupTestServerWithFiles(t)
	// test.txt is not thumbnailable → 404
	url := srv.URL + "/api/files/" + itoa(id) + "/thumbnail"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for non-thumbnailable extension", resp.StatusCode)
	}
}

func TestFileHistoryEndpoint(t *testing.T) {
	srv, archivePath, id := setupTestServerWithFiles(t)
	_ = archivePath
	url := srv.URL + "/api/files/" + itoa(id) + "/history"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var entries []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least 1 history entry referencing the file")
	}
}

func TestFileHistoryEndpoint_NotFound(t *testing.T) {
	srv, _, _ := setupTestServerWithFiles(t)
	url := srv.URL + "/api/files/99999/history"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestFileHistoryEndpoint_InvalidID(t *testing.T) {
	srv, _, _ := setupTestServerWithFiles(t)
	url := srv.URL + "/api/files/notanumber/history"

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// itoa converts int64 to decimal string for URL construction.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
