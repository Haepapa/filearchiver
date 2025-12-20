package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Jobs []Job `yaml:"jobs"`
}

type Job struct {
	Name        string `yaml:"name"`
	Source      string `yaml:"source"`
	Destination string `yaml:"destination"`
}

const lockFile = ".filearchiver.lock"
const dbFile = "filearchiver.db"

var db *sql.DB

func main() {
	inputFlag := flag.String("input", "", "Source directory for one-off job")
	outputFlag := flag.String("output", "", "Destination directory for one-off job")
	configFlag := flag.String("config", "", "Path to YAML config file")
	ignoreFileFlag := flag.String("ignorefile", "", "Path to global .archiveignore file")
	flag.Parse()

	if err := acquireLock(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer releaseLock()

	var err error
	db, err = initDatabase()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database error: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	var jobs []Job

	if *configFlag != "" {
		jobs, err = loadConfig(*configFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
			os.Exit(1)
		}
	} else if *inputFlag != "" && *outputFlag != "" {
		jobs = []Job{{Name: "manual", Source: *inputFlag, Destination: *outputFlag}}
	} else {
		fmt.Fprintf(os.Stderr, "Error: Either provide -config or both -input and -output\n")
		flag.Usage()
		os.Exit(1)
	}

	for _, job := range jobs {
		if err := validatePaths(job.Source, job.Destination); err != nil {
			logHistory(job.Name, "FAILED", err.Error())
			fmt.Fprintf(os.Stderr, "Job '%s' validation failed: %v\n", job.Name, err)
			continue
		}

		ignorePatterns := loadIgnorePatterns(job.Source, *ignoreFileFlag)
		fmt.Printf("Processing job: %s\n", job.Name)
		processJob(job, ignorePatterns)
	}
}

func acquireLock() error {
	if _, err := os.Stat(lockFile); err == nil {
		return fmt.Errorf("lock file exists - another instance may be running")
	}
	f, err := os.Create(lockFile)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func releaseLock() {
	os.Remove(lockFile)
}

func initDatabase() (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		return nil, err
	}

	historyTable := `
	CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		job_name TEXT,
		status TEXT,
		message TEXT
	);`

	registryTable := `
	CREATE TABLE IF NOT EXISTS file_registry (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		original_path TEXT,
		archive_path TEXT,
		file_name TEXT,
		size INTEGER,
		checksum TEXT,
		mod_time DATETIME
	);`

	if _, err := db.Exec(historyTable); err != nil {
		return nil, err
	}
	if _, err := db.Exec(registryTable); err != nil {
		return nil, err
	}

	return db, nil
}

func loadConfig(path string) ([]Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return config.Jobs, nil
}

func validatePaths(source, destination string) error {
	srcAbs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}

	if strings.HasPrefix(dstAbs, srcAbs) {
		return fmt.Errorf("destination cannot be inside source directory")
	}

	if _, err := os.Stat(srcAbs); os.IsNotExist(err) {
		return fmt.Errorf("source directory does not exist")
	}

	return nil
}

func loadIgnorePatterns(sourceDir, globalIgnoreFile string) []string {
	var patterns []string

	if globalIgnoreFile != "" {
		if data, err := os.ReadFile(globalIgnoreFile); err == nil {
			patterns = append(patterns, parseIgnoreFile(string(data))...)
		}
	}

	localIgnore := filepath.Join(sourceDir, ".archiveignore")
	if data, err := os.ReadFile(localIgnore); err == nil {
		patterns = append(patterns, parseIgnoreFile(string(data))...)
	}

	return patterns
}

func parseIgnoreFile(content string) []string {
	var patterns []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns
}

func shouldIgnore(path string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err == nil && matched {
			return true
		}
		if strings.Contains(path, pattern) {
			return true
		}
	}
	return false
}

func processJob(job Job, ignorePatterns []string) {
	err := filepath.Walk(job.Source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logHistory(job.Name, "FAILED", fmt.Sprintf("Error accessing %s: %v", path, err))
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if shouldIgnore(path, ignorePatterns) {
			logHistory(job.Name, "SKIPPED", fmt.Sprintf("Ignored: %s", path))
			return nil
		}

		if err := processFile(path, info, job); err != nil {
			logHistory(job.Name, "FAILED", fmt.Sprintf("Failed to process %s: %v", path, err))
			fmt.Printf("  [FAILED] %s: %v\n", path, err)
		} else {
			logHistory(job.Name, "SUCCESS", fmt.Sprintf("Archived: %s", path))
			fmt.Printf("  [SUCCESS] %s\n", path)
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
	}
}

func processFile(srcPath string, info os.FileInfo, job Job) error {
	ext := strings.TrimPrefix(filepath.Ext(srcPath), ".")
	if ext == "" {
		ext = "no_extension"
	}

	modTime := info.ModTime()
	year := fmt.Sprintf("%04d", modTime.Year())
	month := fmt.Sprintf("%02d", int(modTime.Month()))
	day := fmt.Sprintf("%02d", modTime.Day())

	filename := filepath.Base(srcPath)
	destPath := filepath.Join(job.Destination, ext, year, month, day, filename)

	destPath, err := handleCollision(destPath, job.Destination, ext, year, month, day, filename)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	checksum, err := copyAndVerify(srcPath, destPath)
	if err != nil {
		return err
	}

	if err := os.Remove(srcPath); err != nil {
		os.Remove(destPath)
		return err
	}

	return logFileRegistry(srcPath, destPath, filename, info.Size(), checksum, modTime)
}

func handleCollision(destPath, destination, ext, year, month, day, filename string) (string, error) {
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		return destPath, nil
	}

	duplicatePath := filepath.Join(destination, "_duplicates", ext, year, month, day, filename)
	if _, err := os.Stat(duplicatePath); os.IsNotExist(err) {
		return duplicatePath, nil
	}

	baseExt := filepath.Ext(filename)
	baseName := strings.TrimSuffix(filename, baseExt)

	for i := 1; i <= 99; i++ {
		suffix := fmt.Sprintf("_%02d", i)
		newFilename := baseName + suffix + baseExt
		newPath := filepath.Join(destination, "_duplicates", ext, year, month, day, newFilename)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath, nil
		}
	}

	return "", fmt.Errorf("too many duplicates for file: %s", filename)
}

func copyAndVerify(src, dst string) (string, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer dstFile.Close()

	hash := md5.New()
	writer := io.MultiWriter(dstFile, hash)

	if _, err := io.Copy(writer, srcFile); err != nil {
		return "", err
	}

	checksum := hex.EncodeToString(hash.Sum(nil))

	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return "", err
	}

	if srcInfo.Size() != dstInfo.Size() {
		os.Remove(dst)
		return "", fmt.Errorf("size mismatch after copy")
	}

	return checksum, nil
}

func logHistory(jobName, status, message string) {
	_, err := db.Exec(
		"INSERT INTO history (job_name, status, message) VALUES (?, ?, ?)",
		jobName, status, message,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to log history: %v\n", err)
	}
}

func logFileRegistry(originalPath, archivePath, fileName string, size int64, checksum string, modTime time.Time) error {
	_, err := db.Exec(
		"INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time) VALUES (?, ?, ?, ?, ?, ?)",
		originalPath, archivePath, fileName, size, checksum, modTime,
	)
	return err
}
