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

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
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
	initFlag := flag.Bool("init", false, "Initialize mode: scan and register existing files in output directory")
	setupFlag := flag.Bool("setup", false, "Setup mode: create directories and blank config/ignore files")
	flag.Parse()

	if *setupFlag {
		if err := runSetupMode(*inputFlag, *outputFlag, *configFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Setup complete")
		return
	}

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

	if *initFlag {
		if *outputFlag == "" {
			fmt.Fprintf(os.Stderr, "Error: -init requires -output flag\n")
			flag.Usage()
			os.Exit(1)
		}
		db.Close()
		if err := backupExistingDatabase(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to backup database: %v\n", err)
			os.Exit(1)
		}
		db, err = initDatabase()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create new database: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()
		ignorePatterns := loadIgnorePatterns(*outputFlag, *ignoreFileFlag)
		if err := runInitMode(*outputFlag, ignorePatterns); err != nil {
			fmt.Fprintf(os.Stderr, "Init failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Initialization complete")
		return
	}

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
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(srcPath), "."))
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

func backupExistingDatabase() error {
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		return nil
	}

	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("filearchiver.db.%s", timestamp)
	
	if err := os.Rename(dbFile, backupName); err != nil {
		return fmt.Errorf("failed to backup database: %w", err)
	}
	
	fmt.Printf("Backed up existing database to: %s\n", backupName)
	return nil
}

func runInitMode(outputDir string, ignorePatterns []string) error {
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output path: %w", err)
	}

	if _, err := os.Stat(absOutput); os.IsNotExist(err) {
		return fmt.Errorf("output directory does not exist: %s", absOutput)
	}

	fmt.Printf("Initializing from output directory: %s\n", absOutput)

	duplicatesPath := filepath.Join(absOutput, "_duplicates")
	var allFiles []string
	var duplicateFiles []string
	var regularFiles []string

	err = filepath.Walk(absOutput, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [WARNING] Error accessing %s: %v\n", path, err)
			return nil
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(absOutput, path)
		if err != nil {
			return err
		}

		if strings.HasPrefix(relPath, "_duplicates"+string(filepath.Separator)) {
			duplicateFiles = append(duplicateFiles, path)
		} else {
			regularFiles = append(regularFiles, path)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking directory: %w", err)
	}

	allFiles = append(duplicateFiles, regularFiles...)

	fileCount := 0
	movedCount := 0

	for _, path := range allFiles {
		if shouldIgnore(path, ignorePatterns) {
			fmt.Printf("  [IGNORED] %s\n", path)
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [FAILED] Stat %s: %v\n", path, err)
			continue
		}

		relPath, err := filepath.Rel(absOutput, path)
		if err != nil {
			continue
		}

		fileCount++

		isInDuplicates := strings.HasPrefix(relPath, "_duplicates"+string(filepath.Separator))

		if !isInDuplicates && isValidArchivedPath(relPath) {
			if err := registerExistingFile(path, info); err != nil {
				fmt.Fprintf(os.Stderr, "  [FAILED] Register %s: %v\n", path, err)
			} else {
				fmt.Printf("  [REGISTERED] %s\n", path)
			}
		} else {
			newPath, err := moveToValidPath(path, info, absOutput)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  [FAILED] Move %s: %v\n", path, err)
			} else {
				movedCount++
				if isInDuplicates {
					fmt.Printf("  [MOVED] %s -> %s (from _duplicates)\n", path, newPath)
				} else {
					fmt.Printf("  [MOVED] %s -> %s\n", path, newPath)
				}
			}
		}
	}

	if len(duplicateFiles) > 0 && len(regularFiles) > 0 {
		if _, err := os.Stat(duplicatesPath); err == nil {
			isEmpty := true
			err := filepath.WalkDir(duplicatesPath, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					fmt.Fprintf(os.Stderr, "  [WARNING] Error checking %s: %v\n", path, err)
					return nil
				}
				// Skip the root directory itself
				if path == duplicatesPath {
					return nil
				}
				// If we find any file or non-empty directory, it's not empty
				if !d.IsDir() {
					isEmpty = false
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  [WARNING] Error walking duplicates path: %v\n", err)
			}
			if isEmpty {
				if err := os.RemoveAll(duplicatesPath); err != nil {
					fmt.Fprintf(os.Stderr, "  [WARNING] Failed to remove empty duplicates path: %v\n", err)
				}
			}
		}
	}

	fmt.Printf("Processed %d files (%d moved, %d already valid)\n", fileCount, movedCount, fileCount-movedCount)
	return nil
}

func isValidArchivedPath(relPath string) bool {
	parts := strings.Split(filepath.Clean(relPath), string(filepath.Separator))
	if len(parts) < 5 {
		return false
	}

	year := parts[len(parts)-4]
	month := parts[len(parts)-3]
	day := parts[len(parts)-2]

	if len(year) != 4 || len(month) != 2 || len(day) != 2 {
		return false
	}

	for _, r := range year {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, r := range month {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, r := range day {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

func registerExistingFile(path string, info os.FileInfo) error {
	checksum, err := computeChecksum(path)
	if err != nil {
		return err
	}

	return logFileRegistry(path, path, info.Name(), info.Size(), checksum, info.ModTime())
}

func moveToValidPath(srcPath string, info os.FileInfo, outputRoot string) (string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(srcPath), "."))
	if ext == "" {
		ext = "no_extension"
	}

	modTime := info.ModTime()
	year := fmt.Sprintf("%04d", modTime.Year())
	month := fmt.Sprintf("%02d", int(modTime.Month()))
	day := fmt.Sprintf("%02d", modTime.Day())

	filename := filepath.Base(srcPath)
	destPath := filepath.Join(outputRoot, ext, year, month, day, filename)

	destPath, err := handleCollision(destPath, outputRoot, ext, year, month, day, filename)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", err
	}

	checksum, err := computeChecksum(srcPath)
	if err != nil {
		return "", err
	}

	if err := os.Rename(srcPath, destPath); err != nil {
		return "", fmt.Errorf("failed to move file: %w", err)
	}

	if err := logFileRegistry(srcPath, destPath, filename, info.Size(), checksum, modTime); err != nil {
		return "", err
	}

	return destPath, nil
}

func computeChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func runSetupMode(inputPath, outputPath, configPath string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	if inputPath != "" {
		absInput, err := filepath.Abs(inputPath)
		if err != nil {
			return fmt.Errorf("failed to resolve input path: %w", err)
		}
		if _, err := os.Stat(absInput); os.IsNotExist(err) {
			if err := os.MkdirAll(absInput, 0755); err != nil {
				return fmt.Errorf("failed to create input directory %s: %w", absInput, err)
			}
			fmt.Printf("Created input directory: %s\n", absInput)
		} else {
			fmt.Printf("Input directory already exists: %s\n", absInput)
		}
	}

	if outputPath != "" {
		absOutput, err := filepath.Abs(outputPath)
		if err != nil {
			return fmt.Errorf("failed to resolve output path: %w", err)
		}
		if _, err := os.Stat(absOutput); os.IsNotExist(err) {
			if err := os.MkdirAll(absOutput, 0755); err != nil {
				return fmt.Errorf("failed to create output directory %s: %w", absOutput, err)
			}
			fmt.Printf("Created output directory: %s\n", absOutput)
		} else {
			fmt.Printf("Output directory already exists: %s\n", absOutput)
		}
	}

	configFilePath := configPath
	if configFilePath == "" {
		configFilePath = filepath.Join(workDir, "config.yaml")
	}
	if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
		configTemplate := `# filearchiver configuration file
# Add your archive jobs below
jobs:
  - name: "Example Job"
    source: "/data/input"
    destination: "/data/output"
`
		if err := os.WriteFile(configFilePath, []byte(configTemplate), 0644); err != nil {
			return fmt.Errorf("failed to create config file %s: %w", configFilePath, err)
		}
		fmt.Printf("Created config file: %s\n", configFilePath)
	} else {
		fmt.Printf("Config file already exists: %s\n", configFilePath)
	}

	ignoreFilePath := filepath.Join(workDir, ".archiveignore")
	if _, err := os.Stat(ignoreFilePath); os.IsNotExist(err) {
		ignoreTemplate := `# filearchiver ignore patterns
# Add patterns for files/directories to ignore during archiving
# Examples:
# *.tmp
# .DS_Store
# node_modules/
# Thumbs.db
`
		if err := os.WriteFile(ignoreFilePath, []byte(ignoreTemplate), 0644); err != nil {
			return fmt.Errorf("failed to create ignore file %s: %w", ignoreFilePath, err)
		}
		fmt.Printf("Created ignore file: %s\n", ignoreFilePath)
	} else {
		fmt.Printf("Ignore file already exists: %s\n", ignoreFilePath)
	}

	return nil
}
