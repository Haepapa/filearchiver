package db_test

import (
	"testing"

	"filearchiver/internal/db"
)

// Nav tests share setupDB, seedFiles, and seedHistory helpers from db_test.go.

// ---------------------------------------------------------------------------
// GetNavTypes
// ---------------------------------------------------------------------------

func TestGetNavTypesEmpty(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	types, err := db.GetNavTypes(database)
	if err != nil {
		t.Fatalf("GetNavTypes: %v", err)
	}
	if len(types) != 0 {
		t.Errorf("expected 0 types on empty db, got %d", len(types))
	}
}

func TestGetNavTypesBasic(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	types, err := db.GetNavTypes(database)
	if err != nil {
		t.Fatalf("GetNavTypes: %v", err)
	}
	// Seeded: 3×jpg, 1×mp4, 1×pdf → 3 extension types.
	if len(types) != 3 {
		t.Errorf("expected 3 extension types, got %d", len(types))
	}
}

func TestGetNavTypesSortedByCount(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	types, err := db.GetNavTypes(database)
	if err != nil {
		t.Fatalf("GetNavTypes: %v", err)
	}
	if len(types) == 0 {
		t.Fatal("expected at least one type")
	}
	if types[0].Extension != "jpg" {
		t.Errorf("expected jpg first (most common), got %q", types[0].Extension)
	}
	if types[0].Count != 3 {
		t.Errorf("expected jpg count 3, got %d", types[0].Count)
	}
	for i := 1; i < len(types); i++ {
		if types[i].Count > types[i-1].Count {
			t.Errorf("types not sorted desc at index %d", i)
		}
	}
}

func TestGetNavTypesSizeAggregation(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	types, err := db.GetNavTypes(database)
	if err != nil {
		t.Fatalf("GetNavTypes: %v", err)
	}
	for _, tt := range types {
		if tt.Extension == "jpg" {
			// a.jpg (1024) + c.jpg (512) + e.jpg (2048) = 3584
			want := int64(1024 + 512 + 2048)
			if tt.Size != want {
				t.Errorf("expected jpg total size %d, got %d", want, tt.Size)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// GetNavDates
// ---------------------------------------------------------------------------

func TestGetNavDatesEmpty(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	dates, err := db.GetNavDates(database)
	if err != nil {
		t.Fatalf("GetNavDates: %v", err)
	}
	if len(dates) != 0 {
		t.Errorf("expected 0 years on empty db, got %d", len(dates))
	}
}

func TestGetNavDatesBasic(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	dates, err := db.GetNavDates(database)
	if err != nil {
		t.Fatalf("GetNavDates: %v", err)
	}
	// seedFiles spans 2024 (4 files) and 2025 (1 file).
	if len(dates) != 2 {
		t.Errorf("expected 2 years, got %d", len(dates))
	}
}

func TestGetNavDatesYearOrder(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	dates, err := db.GetNavDates(database)
	if err != nil {
		t.Fatalf("GetNavDates: %v", err)
	}
	if len(dates) < 2 {
		t.Fatal("expected at least 2 years")
	}
	// Most-recent year should come first (descending).
	if dates[0].Year < dates[1].Year {
		t.Errorf("expected descending year order, got %s then %s", dates[0].Year, dates[1].Year)
	}
}

func TestGetNavDatesHasMonths(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	dates, err := db.GetNavDates(database)
	if err != nil {
		t.Fatalf("GetNavDates: %v", err)
	}

	var year2024 *db.NavYearEntry
	for i := range dates {
		if dates[i].Year == "2024" {
			year2024 = &dates[i]
			break
		}
	}
	if year2024 == nil {
		t.Fatal("expected year 2024 in nav dates")
	}
	if len(year2024.Months) == 0 {
		t.Error("expected at least one month under 2024")
	}
}

func TestGetNavDatesTotalCount(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	dates, err := db.GetNavDates(database)
	if err != nil {
		t.Fatalf("GetNavDates: %v", err)
	}

	var total int64
	for _, y := range dates {
		total += y.Count
	}
	if total != 5 {
		t.Errorf("expected sum of year counts to equal 5 (all files), got %d", total)
	}
}

// ---------------------------------------------------------------------------
// GetNavTags
// ---------------------------------------------------------------------------

func TestGetNavTagsDefaultCategories(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	tags, err := db.GetNavTags(database)
	if err != nil {
		t.Fatalf("GetNavTags: %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("expected 3 default tag categories, got %d", len(tags))
	}
}

func TestGetNavTagsWithTags(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	database.Exec(`INSERT INTO tags (name, category_id) VALUES ('Alice', 1)`)
	database.Exec(`INSERT INTO file_tags (file_id, tag_id) VALUES (1, 1)`)
	database.Exec(`INSERT INTO file_tags (file_id, tag_id) VALUES (2, 1)`)

	tags, err := db.GetNavTags(database)
	if err != nil {
		t.Fatalf("GetNavTags: %v", err)
	}

	var people *db.NavTagCategory
	for i := range tags {
		if tags[i].Name == "People" {
			people = &tags[i]
			break
		}
	}
	if people == nil {
		t.Fatal("expected People category")
	}
	if len(people.Tags) != 1 {
		t.Errorf("expected 1 tag under People, got %d", len(people.Tags))
	}
	if people.Tags[0].Name != "Alice" {
		t.Errorf("expected tag Alice, got %q", people.Tags[0].Name)
	}
	if people.Tags[0].Count != 2 {
		t.Errorf("expected Alice count 2, got %d", people.Tags[0].Count)
	}
}

// ---------------------------------------------------------------------------
// GetRecentHistory
// ---------------------------------------------------------------------------

func TestGetRecentHistoryOnlySuccess(t *testing.T) {
	database := setupDB(t)
	seedHistory(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	entries, err := db.GetRecentHistory(database, 0)
	if err != nil {
		t.Fatalf("GetRecentHistory: %v", err)
	}
	// seedHistory has 2 SUCCESS entries.
	if len(entries) != 2 {
		t.Errorf("expected 2 recent SUCCESS entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Status != "SUCCESS" {
			t.Errorf("expected only SUCCESS entries, got %q", e.Status)
		}
	}
}

func TestGetRecentHistoryLimit(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		database.Exec(`INSERT INTO history (job_name, status, message) VALUES ('job','SUCCESS','msg')`)
	}

	entries, err := db.GetRecentHistory(database, 3)
	if err != nil {
		t.Fatalf("GetRecentHistory: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries with limit=3, got %d", len(entries))
	}
}

func TestGetRecentHistoryDefaultLimit(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		database.Exec(`INSERT INTO history (job_name, status, message) VALUES ('job','SUCCESS','msg')`)
	}

	entries, err := db.GetRecentHistory(database, 0) // 0 → default 20
	if err != nil {
		t.Fatalf("GetRecentHistory: %v", err)
	}
	if len(entries) != 20 {
		t.Errorf("expected 20 entries (default limit), got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// ListFiles – Year / Month filters (Phase 2 additions to db/files.go)
// ---------------------------------------------------------------------------

func TestListFilesYearFilter(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{Year: "2024"})
	if err != nil {
		t.Fatalf("ListFiles year filter: %v", err)
	}
	// seedFiles has 4 files in 2024.
	if result.Total != 4 {
		t.Errorf("expected 4 files in 2024, got %d", result.Total)
	}
}

func TestListFilesYearMonthFilter(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	insert := func(name, modTime string) {
		database.Exec(
			`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
			 VALUES (?, ?, ?, 1024, 'x', ?)`,
			"/src/"+name, "/arch/"+name, name, modTime,
		)
	}
	insert("jan1.jpg", "2024-01-10T00:00:00Z")
	insert("jan2.jpg", "2024-01-15T00:00:00Z")
	insert("feb.jpg",  "2024-02-01T00:00:00Z")
	insert("dec.jpg",  "2025-12-01T00:00:00Z")

	result, err := db.ListFiles(database, db.FileListParams{Year: "2024", Month: "01"})
	if err != nil {
		t.Fatalf("ListFiles year+month filter: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected 2 files in 2024-01, got %d", result.Total)
	}
}

func TestListFilesYearDoesNotMatchOtherYears(t *testing.T) {
	database := setupDB(t)
	seedFiles(t, database)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	result, err := db.ListFiles(database, db.FileListParams{Year: "2023"})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("expected 0 files in 2023, got %d", result.Total)
	}
}
