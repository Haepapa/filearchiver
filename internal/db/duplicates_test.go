package db_test

import (
	"database/sql"
	"testing"
	"time"

	"filearchiver/internal/db"
)

// seedDuplicates inserts a primary + duplicate pair into the registry and
// returns their IDs.
func seedDuplicates(t *testing.T, database *sql.DB, identical bool) (primaryID, dupID int64) {
	t.Helper()
	cs1 := "abc123"
	cs2 := "abc123"
	if !identical {
		cs2 = "different999"
	}
	mod := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)

	res, err := database.Exec(
		`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"/src/photo.jpg",
		"/archive/jpg/2024/03/15/photo.jpg",
		"photo.jpg", 10240, cs1, mod,
	)
	if err != nil {
		t.Fatal(err)
	}
	primaryID, _ = res.LastInsertId()

	res2, err := database.Exec(
		`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"/src/photo.jpg",
		"/archive/_duplicates/jpg/2024/03/15/photo.jpg",
		"photo.jpg", 10240, cs2, mod,
	)
	if err != nil {
		t.Fatal(err)
	}
	dupID, _ = res2.LastInsertId()
	return
}

func TestGetDuplicateGroups_Basic(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	seedDuplicates(t, database, true)

	groups, err := db.GetDuplicateGroups(database)
	if err != nil {
		t.Fatalf("GetDuplicateGroups: %v", err)
	}
	if len(groups) == 0 {
		t.Fatal("expected at least one duplicate group")
	}

	g := groups[0]
	if g.FileName != "photo.jpg" {
		t.Errorf("FileName = %q, want photo.jpg", g.FileName)
	}
	if g.Primary == nil {
		t.Error("Primary should not be nil")
	}
	if len(g.Duplicates) != 1 {
		t.Errorf("len(Duplicates) = %d, want 1", len(g.Duplicates))
	}
	if g.Primary.IsDuplicate {
		t.Error("Primary.IsDuplicate should be false")
	}
	if !g.Duplicates[0].IsDuplicate {
		t.Error("Duplicates[0].IsDuplicate should be true")
	}
}

func TestGetDuplicateGroups_NoPrimary(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	// Only a duplicate, no primary.
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	database.Exec(
		`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"/src/orphan.png",
		"/archive/_duplicates/png/2024/01/01/orphan.png",
		"orphan.png", 512, "cs_orphan", mod,
	)

	groups, err := db.GetDuplicateGroups(database)
	if err != nil {
		t.Fatalf("GetDuplicateGroups: %v", err)
	}
	var found bool
	for _, g := range groups {
		if g.FileName == "orphan.png" {
			found = true
			if g.Primary != nil {
				t.Error("orphan group should have nil Primary")
			}
		}
	}
	if !found {
		t.Error("orphan.png group not found")
	}
}

func TestGetDuplicateGroups_Empty(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	// No duplicates — seed only primary files.
	seedFiles(t, database)

	groups, err := db.GetDuplicateGroups(database)
	if err != nil {
		t.Fatal(err)
	}
	// c.jpg in seedFiles IS a duplicate, so at least 1 group expected.
	// Verify all returned groups have at least one entry in Duplicates.
	for _, g := range groups {
		if len(g.Duplicates) == 0 {
			t.Errorf("group %q has no duplicates", g.FileName)
		}
	}
}

func TestDeleteFileRecord(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	primaryID, _ := seedDuplicates(t, database, true)

	if err := db.DeleteFileRecord(database, primaryID); err != nil {
		t.Fatalf("DeleteFileRecord: %v", err)
	}

	// Verify gone
	f, err := db.GetFile(database, primaryID)
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Error("file should be deleted")
	}
}

func TestDeleteFileRecord_NotFound(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	err := db.DeleteFileRecord(database, 99999)
	if err == nil {
		t.Error("expected error for non-existent id")
	}
}

func TestPromoteDuplicateRecord(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	primaryID, dupID := seedDuplicates(t, database, true)

	targetPath := "/archive/jpg/2024/03/15/photo.jpg"

	if err := db.PromoteDuplicateRecord(database, dupID, primaryID, targetPath); err != nil {
		t.Fatalf("PromoteDuplicateRecord: %v", err)
	}

	// Primary should be gone.
	pf, _ := db.GetFile(database, primaryID)
	if pf != nil {
		t.Error("primary should be deleted after promotion")
	}

	// Duplicate should now have the primary's path.
	df, err := db.GetFile(database, dupID)
	if err != nil {
		t.Fatal(err)
	}
	if df == nil {
		t.Fatal("promoted file should still exist")
	}
	if df.ArchivePath != targetPath {
		t.Errorf("ArchivePath = %q, want %q", df.ArchivePath, targetPath)
	}
	if df.IsDuplicate {
		t.Error("promoted file should no longer be a duplicate")
	}
}

func TestPromoteDuplicateRecord_NoPrimary(t *testing.T) {
	database := setupDB(t)
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	_, dupID := seedDuplicates(t, database, true)

	targetPath := "/archive/jpg/2024/03/15/photo.jpg"

	// primaryID=0 means no primary to delete.
	if err := db.PromoteDuplicateRecord(database, dupID, 0, targetPath); err != nil {
		t.Fatalf("PromoteDuplicateRecord (no primary): %v", err)
	}

	df, _ := db.GetFile(database, dupID)
	if df == nil {
		t.Fatal("promoted file should exist")
	}
	if df.ArchivePath != targetPath {
		t.Errorf("ArchivePath = %q, want %q", df.ArchivePath, targetPath)
	}
}

func TestDerivePrimaryPath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			"/archive/_duplicates/jpg/2024/03/15/photo.jpg",
			"/archive/jpg/2024/03/15/photo.jpg",
		},
		{
			"/archive/_duplicates/mp4/2023/12/01/video.mp4",
			"/archive/mp4/2023/12/01/video.mp4",
		},
	}
	for _, c := range cases {
		got := db.DerivePrimaryPath(c.input)
		if got != c.want {
			t.Errorf("DerivePrimaryPath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
