package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"filearchiver/internal/api"
	"filearchiver/internal/db"
)

// setupTestServer creates a test HTTP server backed by a temp database seeded
// with representative data. Returns the server and a cleanup function.
func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	f, err := os.CreateTemp("", "faapi-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	database, err := db.Open(f.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	createSchema(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedTestData(t, database)

	cfg := api.Config{
		DB:          database,
		ArchiveRoot: "/archive",
		Readonly:    false,
		ThumbDir:    t.TempDir(),
	}

	srv := httptest.NewServer(api.NewRouter(cfg))
	t.Cleanup(func() {
		srv.Close()
		database.Close()
		os.Remove(f.Name())
	})
	return srv
}

// setupReadonlyServer creates a test server with Readonly=true.
func setupReadonlyServer(t *testing.T) *httptest.Server {
	t.Helper()

	f, err := os.CreateTemp("", "faapi-ro-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	database, err := db.Open(f.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	createSchema(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seedTestData(t, database)

	cfg := api.Config{
		DB:          database,
		ArchiveRoot: "/archive",
		Readonly:    true,
		ThumbDir:    t.TempDir(),
	}

	srv := httptest.NewServer(api.NewRouter(cfg))
	t.Cleanup(func() {
		srv.Close()
		database.Close()
		os.Remove(f.Name())
	})
	return srv
}

func createSchema(t *testing.T, database *sql.DB) {
	t.Helper()
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
}

func seedTestData(t *testing.T, database *sql.DB) {
	t.Helper()
	files := []struct{ orig, arch, name string; size int64; cs string; mod time.Time }{
		{"/src/a.jpg", "/arch/jpg/2024/01/15/a.jpg", "a.jpg", 1024, "cs1", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)},
		{"/src/b.mp4", "/arch/mp4/2024/02/20/b.mp4", "b.mp4", 204800, "cs2", time.Date(2024, 2, 20, 0, 0, 0, 0, time.UTC)},
		{"/src/c.jpg", "/arch/_duplicates/jpg/2024/03/01/c.jpg", "c.jpg", 512, "cs3", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, f := range files {
		if _, err := database.Exec(
			`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time) VALUES (?, ?, ?, ?, ?, ?)`,
			f.orig, f.arch, f.name, f.size, f.cs, f.mod.Format(time.RFC3339),
		); err != nil {
			t.Fatalf("seed file: %v", err)
		}
	}
	for _, h := range []struct{ job, status, msg string }{
		{"job1", "SUCCESS", "Archived: /src/a.jpg"},
		{"job1", "FAILED", "Error: /src/x.jpg"},
		{"job2", "SUCCESS", "Archived: /src/b.mp4"},
	} {
		if _, err := database.Exec(
			`INSERT INTO history (job_name, status, message) VALUES (?, ?, ?)`,
			h.job, h.status, h.msg,
		); err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /api/stats
// ---------------------------------------------------------------------------

func TestGetStatsOK(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, ok := body["total_files"]; !ok {
		t.Error("missing 'total_files' field")
	}
	if _, ok := body["total_size"]; !ok {
		t.Error("missing 'total_size' field")
	}
	if _, ok := body["total_size_human"]; !ok {
		t.Error("missing 'total_size_human' field")
	}
	if _, ok := body["extensions"]; !ok {
		t.Error("missing 'extensions' field")
	}
}

func TestGetStatsContentType(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestGetStatsTotalFiles(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/stats")
	defer resp.Body.Close()

	var body struct {
		TotalFiles float64 `json:"total_files"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if body.TotalFiles != 3 {
		t.Errorf("expected 3 total files, got %v", body.TotalFiles)
	}
}

// ---------------------------------------------------------------------------
// GET /api/files
// ---------------------------------------------------------------------------

func TestListFilesOK(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files")
	if err != nil {
		t.Fatalf("GET /api/files: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Files []map[string]interface{} `json:"files"`
		Total float64                  `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 3 {
		t.Errorf("expected 3, got %v", body.Total)
	}
	if len(body.Files) != 3 {
		t.Errorf("expected 3 files in response, got %d", len(body.Files))
	}
}

func TestListFilesExtensionFilter(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files?ext=jpg")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Total float64 `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 2 {
		t.Errorf("expected 2 jpg files, got %v", body.Total)
	}
}

func TestListFilesPaginationParams(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files?page=1&per_page=1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Files      []map[string]interface{} `json:"files"`
		Total      float64                  `json:"total"`
		TotalPages float64                  `json:"total_pages"`
		PerPage    float64                  `json:"per_page"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body.Files) != 1 {
		t.Errorf("expected 1 file per page, got %d", len(body.Files))
	}
	if body.Total != 3 {
		t.Errorf("expected total 3, got %v", body.Total)
	}
	if body.TotalPages != 3 {
		t.Errorf("expected 3 total pages, got %v", body.TotalPages)
	}
}

func TestListFilesDuplicatesOnlyFilter(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files?duplicates_only=true")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Total float64 `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 1 {
		t.Errorf("expected 1 duplicate file, got %v", body.Total)
	}
}

func TestListFilesSearchQuery(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files?q=b.mp4")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Total float64 `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 1 {
		t.Errorf("expected 1 result for 'b.mp4', got %v", body.Total)
	}
}

func TestListFilesResponseFields(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files?q=a.jpg")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Files []map[string]interface{} `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Files) == 0 {
		t.Fatal("expected at least one file")
	}

	f := body.Files[0]
	for _, field := range []string{"id", "file_name", "archive_path", "size", "extension", "is_duplicate", "mod_time"} {
		if _, ok := f[field]; !ok {
			t.Errorf("missing field %q in file response", field)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /api/files/{id}
// ---------------------------------------------------------------------------

func TestGetFileOK(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files/1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var file map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&file)
	if file["id"].(float64) != 1 {
		t.Errorf("expected id=1, got %v", file["id"])
	}
}

func TestGetFileNotFound(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files/99999")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetFileInvalidID(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files/notanumber")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GET /api/history
// ---------------------------------------------------------------------------

func TestListHistoryOK(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/history")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Entries []map[string]interface{} `json:"entries"`
		Total   float64                  `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 3 {
		t.Errorf("expected 3 history entries, got %v", body.Total)
	}
}

func TestListHistoryStatusFilter(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/history?status=SUCCESS")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Total float64 `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 2 {
		t.Errorf("expected 2 SUCCESS entries, got %v", body.Total)
	}
}

// ---------------------------------------------------------------------------
// Static file serving
// ---------------------------------------------------------------------------

func TestIndexHTMLServed(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		t.Error("expected a Content-Type header for index.html")
	}
}

func TestStaticJSServed(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/static/app.js")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /static/app.js, got %d", resp.StatusCode)
	}
}
