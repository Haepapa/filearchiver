package db_test

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"filearchiver/internal/db"
)

// setupDB creates a temp SQLite database pre-seeded with the existing CLI
// schema, then runs the web-UI migration. Returns the database and a cleanup
// function.
func setupDB(t *testing.T) *sql.DB {
	t.Helper()

	f, err := os.CreateTemp("", "fatest-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	database, err := db.Open(f.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Create the existing CLI tables (mirroring main.go's initDatabase).
	_, err = database.Exec(`
		CREATE TABLE IF NOT EXISTS history (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			job_name  TEXT,
			status    TEXT,
			message   TEXT
		)`)
	if err != nil {
		t.Fatalf("create history table: %v", err)
	}
	_, err = database.Exec(`
		CREATE TABLE IF NOT EXISTS file_registry (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			original_path TEXT,
			archive_path  TEXT,
			file_name     TEXT,
			size          INTEGER,
			checksum      TEXT,
			mod_time      DATETIME
		)`)
	if err != nil {
		t.Fatalf("create file_registry table: %v", err)
	}

	t.Cleanup(func() {
		database.Close()
		os.Remove(f.Name())
	})

	return database
}

// seedFiles inserts n test file rows and returns the first inserted ID.
func seedFiles(t *testing.T, database *sql.DB) {
	t.Helper()
	files := []struct {
		original, archive, name string
		size                    int64
		checksum                string
		modTime                 time.Time
	}{
		{"/src/a.jpg", "/arch/jpg/2024/01/15/a.jpg", "a.jpg", 1024, "abc1", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)},
		{"/src/b.mp4", "/arch/mp4/2024/02/20/b.mp4", "b.mp4", 204800, "abc2", time.Date(2024, 2, 20, 0, 0, 0, 0, time.UTC)},
		{"/src/c.jpg", "/arch/jpg/_duplicates/jpg/2024/03/01/c.jpg", "c.jpg", 512, "abc3", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"/src/d.pdf", "/arch/pdf/2024/04/10/d.pdf", "d.pdf", 4096, "abc4", time.Date(2024, 4, 10, 0, 0, 0, 0, time.UTC)},
		{"/src/e.jpg", "/arch/jpg/2025/06/01/e.jpg", "e.jpg", 2048, "abc5", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, f := range files {
		_, err := database.Exec(
			`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			f.original, f.archive, f.name, f.size, f.checksum, f.modTime.Format(time.RFC3339),
		)
		if err != nil {
			t.Fatalf("seed file: %v", err)
		}
	}
}

func seedHistory(t *testing.T, database *sql.DB) {
	t.Helper()
	entries := []struct{ job, status, msg string }{
		{"job1", "SUCCESS", "Archived: /src/a.jpg"},
		{"job1", "FAILED", "Error reading /src/x.jpg"},
		{"job2", "SUCCESS", "Archived: /src/b.mp4"},
		{"job2", "SKIPPED", "Ignored: /src/.DS_Store"},
	}
	for _, e := range entries {
		_, err := database.Exec(
			`INSERT INTO history (job_name, status, message) VALUES (?, ?, ?)`,
			e.job, e.status, e.msg,
		)
		if err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Migration tests
// ---------------------------------------------------------------------------

func TestMigrateCreatesTagCategories(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tag_categories`).Scan(&count); err != nil {
		t.Fatalf("query tag_categories: %v", err)
	}
	if count < 3 {
		t.Errorf("expected at least 3 default tag categories, got %d", count)
	}
}

func TestMigrateCreatesTagsTable(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO tags (name, category_id) VALUES ('Alice', 1)`); err != nil {
		t.Fatalf("insert into tags: %v", err)
	}
}

func TestMigrateCreatesFileTagsTable(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	_, err := database.Exec(`INSERT INTO tags (name, category_id) VALUES ('Test', 1)`)
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	_, err = database.Exec(`INSERT INTO file_tags (file_id, tag_id) VALUES (1, 1)`)
	if err != nil {
		t.Fatalf("insert file_tag: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	database := setupDB(t)
	for i := 0; i < 3; i++ {
		if err := db.Migrate(database); err != nil {
			t.Fatalf("Migrate attempt %d: %v", i+1, err)
		}
	}
}

func TestMigrateDefaultCategories(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	rows, err := database.Query(`SELECT name FROM tag_categories ORDER BY name`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}

	want := map[string]bool{"People": true, "Places": true, "Projects": true}
	for _, n := range names {
		delete(want, n)
	}
	if len(want) > 0 {
		t.Errorf("missing default categories: %v", want)
	}
}

// ---------------------------------------------------------------------------
// ListFiles tests
// ---------------------------------------------------------------------------

func TestListFilesReturnsAll(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected 5 total, got %d", result.Total)
	}
	if len(result.Files) != 5 {
		t.Errorf("expected 5 files in page, got %d", len(result.Files))
	}
}

func TestListFilesPagination(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{Page: 1, PerPage: 2})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if len(result.Files) != 2 {
		t.Errorf("expected 2 files on page 1, got %d", len(result.Files))
	}
	if result.TotalPages != 3 {
		t.Errorf("expected 3 total pages, got %d", result.TotalPages)
	}

	result2, err := db.ListFiles(database, db.FileListParams{Page: 3, PerPage: 2})
	if err != nil {
		t.Fatalf("ListFiles page 3: %v", err)
	}
	if len(result2.Files) != 1 {
		t.Errorf("expected 1 file on last page, got %d", len(result2.Files))
	}
}

func TestListFilesFilterByExtension(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{Extension: "jpg"})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("expected 3 jpg files, got %d", result.Total)
	}
	for _, f := range result.Files {
		if f.Extension != "jpg" {
			t.Errorf("unexpected extension %q for file %q", f.Extension, f.FileName)
		}
	}
}

func TestListFilesFilterByQuery(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{Query: "b.mp4"})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 result for 'b.mp4', got %d", result.Total)
	}
}

func TestListFilesFilterDuplicatesOnly(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{DuplicatesOnly: true})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if result.Total != 1 {
		t.Errorf("expected 1 duplicate, got %d", result.Total)
	}
	if !result.Files[0].IsDuplicate {
		t.Error("expected IsDuplicate=true")
	}
}

func TestListFilesDerivedFields(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{Query: "b.mp4"})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	f := result.Files[0]
	if f.Extension != "mp4" {
		t.Errorf("expected extension 'mp4', got %q", f.Extension)
	}
	if f.IsDuplicate {
		t.Error("expected IsDuplicate=false for non-duplicate")
	}
}

func TestListFilesSort(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{Sort: "size", Order: "asc"})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	for i := 1; i < len(result.Files); i++ {
		if result.Files[i].Size < result.Files[i-1].Size {
			t.Errorf("files not sorted by size asc at index %d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// GetFile tests
// ---------------------------------------------------------------------------

func TestGetFileFound(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	f, err := db.GetFile(database, 1)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if f == nil {
		t.Fatal("expected file, got nil")
	}
	if f.ID != 1 {
		t.Errorf("expected ID 1, got %d", f.ID)
	}
}

func TestGetFileNotFound(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	f, err := db.GetFile(database, 99999)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if f != nil {
		t.Error("expected nil for non-existent file")
	}
}

// ---------------------------------------------------------------------------
// GetStats tests
// ---------------------------------------------------------------------------

func TestGetStatsEmpty(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	stats, err := db.GetStats(database)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalFiles != 0 {
		t.Errorf("expected 0 files, got %d", stats.TotalFiles)
	}
	if stats.TotalSize != 0 {
		t.Errorf("expected 0 size, got %d", stats.TotalSize)
	}
}

func TestGetStatsCounts(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	stats, err := db.GetStats(database)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalFiles != 5 {
		t.Errorf("expected 5 total files, got %d", stats.TotalFiles)
	}

	wantSize := int64(1024 + 204800 + 512 + 4096 + 2048)
	if stats.TotalSize != wantSize {
		t.Errorf("expected total size %d, got %d", wantSize, stats.TotalSize)
	}
}

func TestGetStatsExtensions(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	stats, err := db.GetStats(database)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if len(stats.Extensions) == 0 {
		t.Fatal("expected at least one extension stat")
	}
	// jpg should be the most common (3 files)
	if stats.Extensions[0].Extension != "jpg" {
		t.Errorf("expected 'jpg' as most common extension, got %q", stats.Extensions[0].Extension)
	}
	if stats.Extensions[0].Count != 3 {
		t.Errorf("expected jpg count 3, got %d", stats.Extensions[0].Count)
	}
}

func TestGetStatsHumanSize(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	stats, err := db.GetStats(database)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalSizeHuman == "" {
		t.Error("expected non-empty TotalSizeHuman")
	}
}

func TestGetStatsTaggedFiles(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	// Tag two files.
	database.Exec(`INSERT INTO tags (name, category_id) VALUES ('Alice', 1)`)
	database.Exec(`INSERT INTO file_tags (file_id, tag_id) VALUES (1, 1)`)
	database.Exec(`INSERT INTO file_tags (file_id, tag_id) VALUES (2, 1)`)

	stats, err := db.GetStats(database)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TaggedFiles != 2 {
		t.Errorf("expected 2 tagged files, got %d", stats.TaggedFiles)
	}
}

// ---------------------------------------------------------------------------
// ListHistory tests
// ---------------------------------------------------------------------------

func TestListHistoryReturnsAll(t *testing.T) {
	database := setupDB(t)
	seedHistory(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListHistory(database, db.HistoryListParams{})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if result.Total != 4 {
		t.Errorf("expected 4 history entries, got %d", result.Total)
	}
}

func TestListHistoryFilterByStatus(t *testing.T) {
	database := setupDB(t)
	seedHistory(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListHistory(database, db.HistoryListParams{Status: "SUCCESS"})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 SUCCESS entries, got %d", result.Total)
	}
}

func TestListHistoryFilterByJob(t *testing.T) {
	database := setupDB(t)
	seedHistory(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListHistory(database, db.HistoryListParams{JobName: "job2"})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 job2 entries, got %d", result.Total)
	}
}
