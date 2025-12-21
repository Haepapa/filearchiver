package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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
