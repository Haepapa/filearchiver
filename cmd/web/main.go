package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"filearchiver/internal/api"
	"filearchiver/internal/db"
	"filearchiver/internal/proxy"
)

func main() {
	dbPath := flag.String("db", "", "Path to filearchiver.db (required)")
	archivePath := flag.String("archive", "", "Root path of the archive directory (required for file serving)")
	port := flag.Int("port", 8080, "HTTP port to listen on")
	host := flag.String("host", "0.0.0.0", "Bind address")
	readonly := flag.Bool("readonly", false, "Disable all write/delete operations")
	thumbDir := flag.String("thumbdir", "", "Directory to cache thumbnails (default: <db_dir>/.thumbcache)")
	proxyDir := flag.String("proxydir", "", "Directory to store proxy/preview files (default: <db_dir>/.proxycache)")
	noProxy := flag.Bool("no-proxy", false, "Disable background proxy generation worker")
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
	if *proxyDir == "" {
		*proxyDir = filepath.Join(filepath.Dir(*dbPath), ".proxycache")
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Seed proxy settings defaults (no-op if already present).
	if err := db.InitProxySettings(database); err != nil {
		log.Fatalf("Failed to init proxy settings: %v", err)
	}

	cfg := api.Config{
		DB:          database,
		DBPath:      *dbPath,
		ArchiveRoot: *archivePath,
		Readonly:    *readonly,
		ThumbDir:    *thumbDir,
		ProxyDir:    *proxyDir,
	}

	// Start background proxy worker unless disabled.
	if !*readonly && !*noProxy {
		worker := proxy.NewWorker(database, *proxyDir)
		worker.Start(context.Background())
		defer worker.Stop()
		cfg.ProxyWorker = worker
	}

	router := api.NewRouter(cfg)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("filearchiver-web listening on http://%s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
