package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"filearchiver/internal/api"
	"filearchiver/internal/db"
)

func main() {
	dbPath := flag.String("db", "", "Path to filearchiver.db (required)")
	archivePath := flag.String("archive", "", "Root path of the archive directory (required for file serving)")
	port := flag.Int("port", 8080, "HTTP port to listen on")
	host := flag.String("host", "0.0.0.0", "Bind address")
	readonly := flag.Bool("readonly", false, "Disable all write/delete operations")
	thumbDir := flag.String("thumbdir", "", "Directory to cache thumbnails (default: <db_dir>/.thumbcache)")
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "Error: -db flag is required")
		flag.Usage()
		os.Exit(1)
	}
	if *archivePath == "" {
		fmt.Fprintln(os.Stderr, "Error: -archive flag is required")
		flag.Usage()
		os.Exit(1)
	}

	if *thumbDir == "" {
		*thumbDir = filepath.Join(filepath.Dir(*dbPath), ".thumbcache")
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	cfg := api.Config{
		DB:          database,
		DBPath:      *dbPath,
		ArchiveRoot: *archivePath,
		Readonly:    *readonly,
		ThumbDir:    *thumbDir,
	}

	router := api.NewRouter(cfg)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("filearchiver-web listening on http://%s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
