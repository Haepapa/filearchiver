package main_test

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// buildBinary builds the filearchiver binary to a temporary location and returns its path.
func buildBinary(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	binName := "filearchiver"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(binDir, binName)

	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Env = os.Environ()
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, string(out))
	}
	t.Logf("Built binary at %s", binPath)
	return binPath
}

// runBinary runs the built binary with args in a dedicated working directory to isolate lock/db files.
func runBinary(t *testing.T, binPath string, workDir string, args ...string) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = os.Environ()
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, string(out))
	}
	t.Logf("Ran %s in %s with args %v", binPath, workDir, args)
}

func writeFileWithModTime(t *testing.T, dir, name, content string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// Update mtime to deterministic value
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	t.Logf("Created %s (mtime %s)", path, modTime.Format(time.RFC3339))
	return path
}

func expectMoved(t *testing.T, dstRoot, filename string, modTime time.Time) {
	t.Helper()
	extra := filepath.Ext(filename)
	if extra != "" {
		extra = extra[1:]
	} else {
		extra = "no_extension"
	}
	year := modTime.Format("2006")
	month := modTime.Format("01")
	day := modTime.Format("02")
	destPath := filepath.Join(dstRoot, extra, year, month, day, filepath.Base(filename))
	if _, err := os.Stat(destPath); err != nil {
		t.Fatalf("expected archived file at %s: %v", destPath, err)
	}
	t.Logf("Archived %s -> %s", filename, destPath)
}

func isDirEmpty(t *testing.T, dir string) bool {
	t.Helper()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatalf("open dir: %v", err)
	}
	defer f.Close()
	// Read one entry
	names, err := f.Readdirnames(1)
	if err != nil {
		t.Logf("Directory %s is empty", dir)
		return true // EOF => empty
	}
	empty := len(names) == 0
	t.Logf("Directory %s empty=%t", dir, empty)
	return empty
}

func TestOneOffRun(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	src := filepath.Join(work, "src")
	dst := filepath.Join(work, "dst")
	if err := os.MkdirAll(src, 0755); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(dst, 0755); err != nil { t.Fatal(err) }

	mt := time.Date(2023, 7, 14, 12, 0, 0, 0, time.Local)
	f1 := writeFileWithModTime(t, src, "doc.txt", "hello", mt)
	f2 := writeFileWithModTime(t, src, "image.jpeg", "data", mt)

	runBinary(t, bin, work, "-input", src, "-output", dst)

	if !isDirEmpty(t, src) {
		t.Fatalf("expected source to be empty after archive")
	}
	t.Logf("Source emptied: %s", src)
	expectMoved(t, dst, f1, mt)
	expectMoved(t, dst, f2, mt)
}

func TestRunUsingConfig(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	src := filepath.Join(work, "cfgsrc")
	dst := filepath.Join(work, "cfgdst")
	if err := os.MkdirAll(src, 0755); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(dst, 0755); err != nil { t.Fatal(err) }

	mt := time.Date(2022, 1, 2, 3, 4, 5, 0, time.Local)
	f := writeFileWithModTime(t, src, "report.pdf", "content", mt)

	cfg := []byte("jobs:\n  - name: testrun\n    source: \"" + src + "\"\n    destination: \"" + dst + "\"\n")
	cfgPath := filepath.Join(work, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0644); err != nil { t.Fatal(err) }
	t.Logf("Wrote config %s", cfgPath)

	runBinary(t, bin, work, "-config", cfgPath)

	if !isDirEmpty(t, src) {
		t.Fatalf("expected source to be empty after archive via config")
	}
	t.Logf("Source emptied: %s", src)
	expectMoved(t, dst, f, mt)
}

func TestRunWithIgnore(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	src := filepath.Join(work, "ignsrc")
	dst := filepath.Join(work, "igndst")
	if err := os.MkdirAll(src, 0755); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(dst, 0755); err != nil { t.Fatal(err) }

	mt := time.Date(2024, 10, 11, 9, 0, 0, 0, time.Local)
	_ = writeFileWithModTime(t, src, "move.txt", "m", mt)
	keep := writeFileWithModTime(t, src, "keep.tmp", "k", mt)
	// Local ignore file to skip *.tmp
	if err := os.WriteFile(filepath.Join(src, ".archiveignore"), []byte("*.tmp\n"), 0644); err != nil { t.Fatal(err) }
	t.Logf("Using ignore file: %s", filepath.Join(src, ".archiveignore"))

	runBinary(t, bin, work, "-input", src, "-output", dst)

	// keep.tmp should remain, move.txt should be archived
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("expected ignored file to remain: %v", err)
	}
	t.Logf("Ignored remained: %s", keep)
	// Source may have the ignored file, but ensure move.txt is gone
	if _, err := os.Stat(filepath.Join(src, "move.txt")); err == nil {
		t.Fatalf("expected move.txt to be moved from source")
	}
	t.Logf("Moved from source: %s", filepath.Join(src, "move.txt"))
}

func TestInitModeWithValidPaths(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2023, 7, 14, 12, 0, 0, 0, time.Local)
	
	validPath := filepath.Join(dst, "txt", "2023", "07", "14", "test.txt")
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(validPath, []byte("content"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(validPath, mt, mt); err != nil { t.Fatal(err) }
	t.Logf("Created valid file: %s", validPath)

	runBinary(t, bin, work, "-init", "-output", dst)

	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("expected file to remain at valid path: %v", err)
	}
	t.Logf("File remained at valid path: %s", validPath)
}

func TestInitModeWithInvalidPaths(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")
	if err := os.MkdirAll(dst, 0755); err != nil { t.Fatal(err) }

	mt := time.Date(2022, 3, 5, 10, 30, 0, 0, time.Local)
	
	invalidPath := filepath.Join(dst, "random_file.pdf")
	if err := os.WriteFile(invalidPath, []byte("data"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(invalidPath, mt, mt); err != nil { t.Fatal(err) }
	t.Logf("Created invalid file: %s", invalidPath)

	runBinary(t, bin, work, "-init", "-output", dst)

	if _, err := os.Stat(invalidPath); err == nil {
		t.Fatalf("expected file to be moved from invalid path")
	}
	
	expectedPath := filepath.Join(dst, "pdf", "2022", "03", "05", "random_file.pdf")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected file to be moved to valid path %s: %v", expectedPath, err)
	}
	t.Logf("File moved to valid path: %s", expectedPath)
}

func TestInitModeWithMixedPaths(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt1 := time.Date(2023, 1, 15, 8, 0, 0, 0, time.Local)
	mt2 := time.Date(2023, 2, 20, 14, 0, 0, 0, time.Local)

	validPath := filepath.Join(dst, "jpg", "2023", "01", "15", "photo.jpg")
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(validPath, []byte("image"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(validPath, mt1, mt1); err != nil { t.Fatal(err) }

	invalidPath := filepath.Join(dst, "docs", "report.docx")
	if err := os.MkdirAll(filepath.Dir(invalidPath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(invalidPath, []byte("document"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(invalidPath, mt2, mt2); err != nil { t.Fatal(err) }

	t.Logf("Created valid: %s, invalid: %s", validPath, invalidPath)

	runBinary(t, bin, work, "-init", "-output", dst)

	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("expected valid file to remain: %v", err)
	}
	
	if _, err := os.Stat(invalidPath); err == nil {
		t.Fatalf("expected invalid file to be moved")
	}

	expectedPath := filepath.Join(dst, "docx", "2023", "02", "20", "report.docx")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected moved file at %s: %v", expectedPath, err)
	}
	t.Logf("Valid remained, invalid moved to: %s", expectedPath)
}

func TestInitModeWithoutOutputOrConfigFlag(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()

	cmd := exec.Command(bin, "-init")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail without -output or -config flag")
	}

	if !strings.Contains(string(out), "-init requires -output flag or -config flag") {
		t.Fatalf("expected error message about missing -output or -config flag, got: %s", string(out))
	}
	t.Logf("Correctly rejected -init without -output or -config: %s", string(out))
}

func TestInitModeWithConfig(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2024, 3, 10, 9, 0, 0, 0, time.Local)

	validPath := filepath.Join(dst, "txt", "2024", "03", "10", "hello.txt")
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validPath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(validPath, mt, mt); err != nil {
		t.Fatal(err)
	}

	invalidPath := filepath.Join(dst, "misplaced.pdf")
	if err := os.WriteFile(invalidPath, []byte("pdf data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(invalidPath, mt, mt); err != nil {
		t.Fatal(err)
	}
	t.Logf("Created valid: %s, invalid: %s", validPath, invalidPath)

	cfgContent := "jobs:\n  - name: initjob\n    source: \"/dev/null\"\n    destination: \"" + dst + "\"\n"
	cfgPath := filepath.Join(work, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("Wrote config: %s", cfgPath)

	runBinary(t, bin, work, "-init", "-config", cfgPath)

	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("expected valid file to remain at %s: %v", validPath, err)
	}
	t.Logf("Valid file remained: %s", validPath)

	if _, err := os.Stat(invalidPath); err == nil {
		t.Fatalf("expected misplaced file to be moved from %s", invalidPath)
	}

	expectedPath := filepath.Join(dst, "pdf", "2024", "03", "10", "misplaced.pdf")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected misplaced file to be moved to %s: %v", expectedPath, err)
	}
	t.Logf("Misplaced file moved to: %s", expectedPath)
}

func TestInitModeWithConfigMultipleJobs(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst1 := filepath.Join(work, "archive1")
	dst2 := filepath.Join(work, "archive2")

	mt := time.Date(2024, 6, 15, 12, 0, 0, 0, time.Local)

	// Place a misplaced file in each destination.
	for _, dst := range []string{dst1, dst2} {
		if err := os.MkdirAll(dst, 0755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dst, "stray.jpg")
		if err := os.WriteFile(p, []byte("img"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		t.Logf("Created stray file: %s", p)
	}

	cfgContent := "jobs:\n" +
		"  - name: job1\n    source: \"/dev/null\"\n    destination: \"" + dst1 + "\"\n" +
		"  - name: job2\n    source: \"/dev/null\"\n    destination: \"" + dst2 + "\"\n"
	cfgPath := filepath.Join(work, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("Wrote config: %s", cfgPath)

	runBinary(t, bin, work, "-init", "-config", cfgPath)

	for _, dst := range []string{dst1, dst2} {
		stray := filepath.Join(dst, "stray.jpg")
		if _, err := os.Stat(stray); err == nil {
			t.Fatalf("expected stray.jpg to be moved in %s", dst)
		}
		expected := filepath.Join(dst, "jpg", "2024", "06", "15", "stray.jpg")
		if _, err := os.Stat(expected); err != nil {
			t.Fatalf("expected stray.jpg moved to %s: %v", expected, err)
		}
		t.Logf("Stray file correctly moved in %s", dst)
	}
}

func TestInitModeWithConfigFileMissing(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	missingConfig := filepath.Join(work, "nonexistent.yaml")

	cmd := exec.Command(bin, "-init", "-config", missingConfig)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail with missing config file")
	}
	t.Logf("Correctly failed with missing config: %s", string(out))
}

func TestInitModeOutputFlagStillWorks(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2024, 5, 20, 10, 0, 0, 0, time.Local)
	validPath := filepath.Join(dst, "png", "2024", "05", "20", "img.png")
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validPath, []byte("pngdata"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(validPath, mt, mt); err != nil {
		t.Fatal(err)
	}
	t.Logf("Created valid file: %s", validPath)

	runBinary(t, bin, work, "-init", "-output", dst)

	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("expected file to remain at valid path: %v", err)
	}
	t.Logf("Existing -output behavior still works: %s", validPath)
}

func TestInitModeWithNonExistentOutput(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	nonExistent := filepath.Join(work, "does_not_exist")

	cmd := exec.Command(bin, "-init", "-output", nonExistent)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected command to fail with non-existent output directory")
	}
	
	if !strings.Contains(string(out), "does not exist") {
		t.Fatalf("expected error about non-existent directory, got: %s", string(out))
	}
	t.Logf("Correctly rejected non-existent output: %s", string(out))
}

func TestInitModeSkipsDuplicatesFolder(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2023, 5, 10, 12, 0, 0, 0, time.Local)

	duplicatePath := filepath.Join(dst, "_duplicates", "txt", "file.txt")
	if err := os.MkdirAll(filepath.Dir(duplicatePath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(duplicatePath, []byte("dup"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(duplicatePath, mt, mt); err != nil { t.Fatal(err) }
	t.Logf("Created file in _duplicates: %s", duplicatePath)

	runBinary(t, bin, work, "-init", "-output", dst)

	if _, err := os.Stat(duplicatePath); err == nil {
		t.Fatalf("expected duplicate file to be moved, not remain in _duplicates")
	}
	
	expectedPath := filepath.Join(dst, "txt", "2023", "05", "10", "file.txt")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected file to be moved to valid path %s: %v", expectedPath, err)
	}
	t.Logf("Duplicate file correctly processed and moved: %s", expectedPath)
}

func TestInitModeDoesNotAffectNormalOperation(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	src := filepath.Join(work, "src")
	dst := filepath.Join(work, "dst")
	if err := os.MkdirAll(src, 0755); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(dst, 0755); err != nil { t.Fatal(err) }

	mt := time.Date(2023, 6, 1, 10, 0, 0, 0, time.Local)
	f := writeFileWithModTime(t, src, "normal.txt", "test", mt)

	runBinary(t, bin, work, "-input", src, "-output", dst)

	if !isDirEmpty(t, src) {
		t.Fatalf("expected source to be empty after normal archive")
	}
	expectMoved(t, dst, f, mt)
	t.Logf("Normal operation works correctly after -init implementation")
}

func TestInitModeBackupsExistingDatabase(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")
	if err := os.MkdirAll(dst, 0755); err != nil { t.Fatal(err) }

	dbPath := filepath.Join(work, "filearchiver.db")
	validDB, err := sql.Open("sqlite", dbPath)
	if err != nil { t.Fatal(err) }
	_, err = validDB.Exec("CREATE TABLE test (id INTEGER)")
	if err != nil { t.Fatal(err) }
	validDB.Close()
	t.Logf("Created existing database: %s", dbPath)

	mt := time.Date(2023, 7, 1, 10, 0, 0, 0, time.Local)
	validPath := filepath.Join(dst, "txt", "2023", "07", "01", "test.txt")
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(validPath, []byte("content"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(validPath, mt, mt); err != nil { t.Fatal(err) }

	runBinary(t, bin, work, "-init", "-output", dst)

	files, err := filepath.Glob(filepath.Join(work, "filearchiver.db.*"))
	if err != nil { t.Fatal(err) }
	
	if len(files) != 1 {
		t.Fatalf("expected 1 backup database file, found %d", len(files))
	}
	t.Logf("Backup database created: %s", files[0])

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected new database to exist: %v", err)
	}
	t.Logf("New database created: %s", dbPath)
}

func TestInitModeProcessesDuplicatesFirst(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2023, 8, 15, 14, 30, 0, 0, time.Local)

	validPath := filepath.Join(dst, "txt", "2023", "08", "15", "file.txt")
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(validPath, []byte("original"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(validPath, mt, mt); err != nil { t.Fatal(err) }

	duplicatePath := filepath.Join(dst, "_duplicates", "txt", "file.txt")
	if err := os.MkdirAll(filepath.Dir(duplicatePath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(duplicatePath, []byte("duplicate"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(duplicatePath, mt, mt); err != nil { t.Fatal(err) }

	t.Logf("Created valid: %s, duplicate: %s", validPath, duplicatePath)

	runBinary(t, bin, work, "-init", "-output", dst)

	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("expected original file to remain: %v", err)
	}

	if _, err := os.Stat(duplicatePath); err == nil {
		t.Fatalf("expected duplicate file to be moved from _duplicates")
	}

	newDupPath := filepath.Join(dst, "_duplicates", "txt", "2023", "08", "15", "file.txt")
	if _, err := os.Stat(newDupPath); err != nil {
		t.Fatalf("expected duplicate to be moved to new _duplicates location: %v", err)
	}
	t.Logf("Duplicate correctly handled: %s", newDupPath)
}

func TestInitModeHandlesMultipleDuplicateCollisions(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2023, 9, 20, 10, 0, 0, 0, time.Local)

	validPath := filepath.Join(dst, "pdf", "2023", "09", "20", "report.pdf")
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(validPath, []byte("v1"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(validPath, mt, mt); err != nil { t.Fatal(err) }

	dup1Path := filepath.Join(dst, "_duplicates", "subdir1", "report.pdf")
	if err := os.MkdirAll(filepath.Dir(dup1Path), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(dup1Path, []byte("v2"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(dup1Path, mt, mt); err != nil { t.Fatal(err) }

	dup2Path := filepath.Join(dst, "_duplicates", "subdir2", "report.pdf")
	if err := os.MkdirAll(filepath.Dir(dup2Path), 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(dup2Path, []byte("v3"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(dup2Path, mt, mt); err != nil { t.Fatal(err) }

	t.Logf("Created valid + 2 duplicates")

	runBinary(t, bin, work, "-init", "-output", dst)

	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("expected original to remain: %v", err)
	}

	newDup1 := filepath.Join(dst, "_duplicates", "pdf", "2023", "09", "20", "report.pdf")
	newDup2 := filepath.Join(dst, "_duplicates", "pdf", "2023", "09", "20", "report_01.pdf")

	foundDups := 0
	if _, err := os.Stat(newDup1); err == nil {
		foundDups++
		t.Logf("Found duplicate at: %s", newDup1)
	}
	if _, err := os.Stat(newDup2); err == nil {
		foundDups++
		t.Logf("Found duplicate at: %s", newDup2)
	}

	if foundDups != 2 {
		t.Fatalf("expected 2 duplicates with collision handling, found %d", foundDups)
	}
}

// TestInitModePreservesExistingDuplicates verifies that files already sitting in
// _duplicates/<ext>/<YYYY>/<MM>/<DD>/ that are genuine duplicates of a main-archive
// file are registered in place and NOT moved again.
func TestInitModePreservesExistingDuplicates(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2024, 3, 5, 9, 0, 0, 0, time.Local)

	// Main archive file.
	originalPath := filepath.Join(dst, "pdf", "2024", "03", "05", "report.pdf")
	if err := os.MkdirAll(filepath.Dir(originalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalPath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(originalPath, mt, mt); err != nil {
		t.Fatal(err)
	}

	// Correctly-structured duplicate (same name as original in _duplicates).
	dup1Path := filepath.Join(dst, "_duplicates", "pdf", "2024", "03", "05", "report.pdf")
	if err := os.MkdirAll(filepath.Dir(dup1Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup1Path, []byte("dup1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dup1Path, mt, mt); err != nil {
		t.Fatal(err)
	}

	// Correctly-structured duplicate with _01 collision suffix.
	dup2Path := filepath.Join(dst, "_duplicates", "pdf", "2024", "03", "05", "report_01.pdf")
	if err := os.MkdirAll(filepath.Dir(dup2Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dup2Path, []byte("dup2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dup2Path, mt, mt); err != nil {
		t.Fatal(err)
	}

	t.Logf("Created original: %s, dup1: %s, dup2: %s", originalPath, dup1Path, dup2Path)

	runBinary(t, bin, work, "-init", "-output", dst)

	// Original must stay put.
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("expected original file to remain at %s: %v", originalPath, err)
	}

	// Both duplicates must remain in _duplicates — not moved elsewhere.
	if _, err := os.Stat(dup1Path); err != nil {
		t.Fatalf("expected dup1 to remain in _duplicates at %s: %v", dup1Path, err)
	}
	if _, err := os.Stat(dup2Path); err != nil {
		t.Fatalf("expected dup2 to remain in _duplicates at %s: %v", dup2Path, err)
	}

	// Ensure they were not also copied to an incorrect main-path location.
	wrongPath := filepath.Join(dst, "pdf", "2024", "03", "05", "report_01.pdf")
	if _, err := os.Stat(wrongPath); err == nil {
		t.Fatalf("dup2 should NOT have been moved to main path %s", wrongPath)
	}

	t.Logf("Existing duplicates correctly preserved in _duplicates")
}

// TestInitModePreservesExistingDuplicatesWithDSuffix verifies that files in _duplicates
// that use the _dNNNN suffix format (e.g. report_d0001.pdf) are recognised as genuine
// duplicates of the corresponding main-archive file and are NOT moved.
func TestInitModePreservesExistingDuplicatesWithDSuffix(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")

	mt := time.Date(2023, 1, 2, 10, 0, 0, 0, time.Local)

	// Main archive file.
	originalPath := filepath.Join(dst, "crm", "2023", "01", "02", "20221231 - NewYear2022 - part 45.CRM")
	if err := os.MkdirAll(filepath.Dir(originalPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalPath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(originalPath, mt, mt); err != nil {
		t.Fatal(err)
	}

	// Duplicate with _d0001 suffix already sitting inside _duplicates.
	dupPath := filepath.Join(dst, "_duplicates", "crm", "2023", "01", "02", "20221231 - NewYear2022 - part 45_d0001.CRM")
	if err := os.MkdirAll(filepath.Dir(dupPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dupPath, []byte("duplicate"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dupPath, mt, mt); err != nil {
		t.Fatal(err)
	}

	runBinary(t, bin, work, "-init", "-output", dst)

	// Original must stay put.
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("expected original to remain at %s: %v", originalPath, err)
	}

	// Duplicate must remain in _duplicates — not moved elsewhere.
	if _, err := os.Stat(dupPath); err != nil {
		t.Fatalf("expected _d0001 duplicate to remain at %s: %v", dupPath, err)
	}

	t.Logf("_d#### duplicate correctly preserved in _duplicates")
}

func TestInitModeHonorsIgnoreFile(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	dst := filepath.Join(work, "dst")
	if err := os.MkdirAll(dst, 0755); err != nil { t.Fatal(err) }

	mt := time.Date(2023, 10, 15, 12, 0, 0, 0, time.Local)

	invalidPath1 := filepath.Join(dst, "document.txt")
	if err := os.WriteFile(invalidPath1, []byte("should be moved"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(invalidPath1, mt, mt); err != nil { t.Fatal(err) }

	invalidPath2 := filepath.Join(dst, "keepme.tmp")
	if err := os.WriteFile(invalidPath2, []byte("should be ignored"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(invalidPath2, mt, mt); err != nil { t.Fatal(err) }

	invalidPath3 := filepath.Join(dst, ".DS_Store")
	if err := os.WriteFile(invalidPath3, []byte("should be ignored"), 0644); err != nil { t.Fatal(err) }
	if err := os.Chtimes(invalidPath3, mt, mt); err != nil { t.Fatal(err) }

	ignoreFilePath := filepath.Join(work, ".archiveignore")
	if err := os.WriteFile(ignoreFilePath, []byte("*.tmp\n.DS_Store\n"), 0644); err != nil { t.Fatal(err) }
	t.Logf("Created ignore file: %s", ignoreFilePath)

	t.Logf("Created files: %s (move), %s (ignore), %s (ignore)", invalidPath1, invalidPath2, invalidPath3)

	runBinary(t, bin, work, "-init", "-output", dst, "-ignorefile", ignoreFilePath)

	if _, err := os.Stat(invalidPath1); err == nil {
		t.Fatalf("expected document.txt to be moved from invalid path")
	}

	expectedPath := filepath.Join(dst, "txt", "2023", "10", "15", "document.txt")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected document.txt to be moved to valid path %s: %v", expectedPath, err)
	}
	t.Logf("document.txt correctly moved to: %s", expectedPath)

	if _, err := os.Stat(invalidPath2); err != nil {
		t.Fatalf("expected keepme.tmp to remain at original location (ignored): %v", err)
	}
	t.Logf("keepme.tmp correctly ignored and remained at: %s", invalidPath2)

	if _, err := os.Stat(invalidPath3); err != nil {
		t.Fatalf("expected .DS_Store to remain at original location (ignored): %v", err)
	}
	t.Logf(".DS_Store correctly ignored and remained at: %s", invalidPath3)
}

func TestSetupModeCreatesDirectories(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	src := filepath.Join(work, "newsrc")
	dst := filepath.Join(work, "newdst")

	cmd := exec.Command(bin, "-setup", ".", "-input", src, "-output", dst)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	if _, err := os.Stat(src); os.IsNotExist(err) {
		t.Fatalf("expected input directory to be created: %v", err)
	}
	t.Logf("Input directory created: %s", src)

	if _, err := os.Stat(dst); os.IsNotExist(err) {
		t.Fatalf("expected output directory to be created: %v", err)
	}
	t.Logf("Output directory created: %s", dst)
}

func TestSetupModeCreatesConfigFile(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()

	cmd := exec.Command(bin, "-setup", ".")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	configPath := filepath.Join(work, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "jobs:") {
		t.Fatalf("expected config file to contain 'jobs:', got: %s", content)
	}
	t.Logf("Config file created with valid content: %s", configPath)
}

func TestSetupModeCreatesIgnoreFile(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()

	cmd := exec.Command(bin, "-setup", ".")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	ignorePath := filepath.Join(work, ".archiveignore")
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("expected ignore file to be created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "ignore patterns") {
		t.Fatalf("expected ignore file to contain template text, got: %s", content)
	}
	t.Logf("Ignore file created with valid content: %s", ignorePath)
}

func TestSetupModeWithCustomConfigPath(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	configDir := filepath.Join(work, "custom")
	if err := os.MkdirAll(configDir, 0755); err != nil { t.Fatal(err) }
	
	customConfigPath := filepath.Join(configDir, "myconfig.yaml")

	cmd := exec.Command(bin, "-setup", ".", "-config", customConfigPath)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	data, err := os.ReadFile(customConfigPath)
	if err != nil {
		t.Fatalf("expected custom config file to be created: %v", err)
	}

	if !strings.Contains(string(data), "jobs:") {
		t.Fatalf("expected custom config file to contain 'jobs:'")
	}
	t.Logf("Custom config file created: %s", customConfigPath)
}

func TestSetupModeDoesNotOverwriteExisting(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	src := filepath.Join(work, "src")
	if err := os.MkdirAll(src, 0755); err != nil { t.Fatal(err) }

	configPath := filepath.Join(work, "config.yaml")
	originalContent := "# original config\njobs: []"
	if err := os.WriteFile(configPath, []byte(originalContent), 0644); err != nil { t.Fatal(err) }

	ignorePath := filepath.Join(work, ".archiveignore")
	originalIgnore := "# original ignore\n*.bak"
	if err := os.WriteFile(ignorePath, []byte(originalIgnore), 0644); err != nil { t.Fatal(err) }

	cmd := exec.Command(bin, "-setup", ".", "-input", src)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if string(configData) != originalContent {
		t.Fatalf("config file was overwritten, expected: %s, got: %s", originalContent, string(configData))
	}
	t.Logf("Config file not overwritten")

	ignoreData, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("failed to read ignore file: %v", err)
	}
	if string(ignoreData) != originalIgnore {
		t.Fatalf("ignore file was overwritten, expected: %s, got: %s", originalIgnore, string(ignoreData))
	}
	t.Logf("Ignore file not overwritten")
}

func TestSetupModeDoesNotImpactNormalOperation(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	src := filepath.Join(work, "src")
	dst := filepath.Join(work, "dst")

	cmd := exec.Command(bin, "-setup", ".", "-input", src, "-output", dst)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup completed")

	mt := time.Date(2023, 7, 14, 12, 0, 0, 0, time.Local)
	f := writeFileWithModTime(t, src, "test.txt", "content", mt)

	runBinary(t, bin, work, "-input", src, "-output", dst)

	if !isDirEmpty(t, src) {
		t.Fatalf("expected source to be empty after archive")
	}
	expectMoved(t, dst, f, mt)
	t.Logf("Normal operation works after setup")
}

func TestSetupModeCreatesFilesInSpecifiedPath(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	setupDir := filepath.Join(work, "conf", "config")

	cmd := exec.Command(bin, "-setup", setupDir)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	configPath := filepath.Join(setupDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file at %s: %v", configPath, err)
	}
	if !strings.Contains(string(data), "jobs:") {
		t.Fatalf("expected config file to contain 'jobs:', got: %s", string(data))
	}
	t.Logf("Config file created at specified path: %s", configPath)

	ignorePath := filepath.Join(setupDir, ".archiveignore")
	ignoreData, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("expected ignore file at %s: %v", ignorePath, err)
	}
	if !strings.Contains(string(ignoreData), "ignore patterns") {
		t.Fatalf("expected ignore file template text, got: %s", string(ignoreData))
	}
	t.Logf("Ignore file created at specified path: %s", ignorePath)
}

func TestSetupModeCreatesSetupDirectory(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	setupDir := filepath.Join(work, "new", "nested", "dir")

	if _, err := os.Stat(setupDir); err == nil {
		t.Fatalf("setup dir should not exist before test")
	}

	cmd := exec.Command(bin, "-setup", setupDir)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	if _, err := os.Stat(setupDir); os.IsNotExist(err) {
		t.Fatalf("expected setup directory to be created: %v", err)
	}
	t.Logf("Setup directory auto-created: %s", setupDir)

	if _, err := os.Stat(filepath.Join(setupDir, "config.yaml")); err != nil {
		t.Fatalf("expected config.yaml inside created directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(setupDir, ".archiveignore")); err != nil {
		t.Fatalf("expected .archiveignore inside created directory: %v", err)
	}
}

func TestSetupModePathDoesNotWriteToWorkingDir(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	setupDir := filepath.Join(work, "conf")

	cmd := exec.Command(bin, "-setup", setupDir)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	// config and ignore must NOT be in the working directory
	if _, err := os.Stat(filepath.Join(work, "config.yaml")); err == nil {
		t.Fatalf("config.yaml should not be created in working directory when setup path is given")
	}
	if _, err := os.Stat(filepath.Join(work, ".archiveignore")); err == nil {
		t.Fatalf(".archiveignore should not be created in working directory when setup path is given")
	}
	t.Logf("Correctly wrote files only to setup path, not working dir")
}

func TestSetupModePathWithExplicitConfig(t *testing.T) {
	bin := buildBinary(t)
	work := t.TempDir()
	setupDir := filepath.Join(work, "conf")
	customConfigDir := filepath.Join(work, "other")
	if err := os.MkdirAll(customConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	customConfigPath := filepath.Join(customConfigDir, "myconfig.yaml")

	cmd := exec.Command(bin, "-setup", setupDir, "-config", customConfigPath)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup command failed: %v\n%s", err, string(out))
	}
	t.Logf("Setup output: %s", string(out))

	// -config overrides config file location
	if _, err := os.ReadFile(customConfigPath); err != nil {
		t.Fatalf("expected config file at explicit -config path %s: %v", customConfigPath, err)
	}
	t.Logf("Config written to explicit -config path: %s", customConfigPath)

	// .archiveignore still goes under setupDir
	ignorePath := filepath.Join(setupDir, ".archiveignore")
	if _, err := os.ReadFile(ignorePath); err != nil {
		t.Fatalf("expected .archiveignore at setup path %s: %v", ignorePath, err)
	}
	t.Logf("Ignore file written to setup path: %s", ignorePath)
}

