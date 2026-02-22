# filearchiver

A small, fast CLI that archives files from a source directory into a structured destination, verifies integrity, and records actions in a local SQLite database. Files are organized by extension and modification date, collisions are handled safely, and ignore patterns are supported.

## Features
- One-off runs with flags or batch runs via YAML config
- Integrity verification (MD5) on copy, then source deletion
- Collision handling using _duplicates and numeric suffixes
- Per-run history and file registry stored in filearchiver.db
- Local and global ignore files (.archiveignore)
- Single-instance protection via .filearchiver.lock
- Initialize mode to scan and register existing archived files

## Install
- Prebuilt binaries: After a branch is merged into test or prod, go to GitHub → Actions → “Build binaries” → select the latest run → download artifacts for your OS/arch (filearchiver-<os>-<arch>). Windows binaries have .exe.
- Build from source: Requires Go 1.21+
  - git clone <this repo>
  - go build -o filearchiver ./
- Docker: Pull the container image
  - docker pull ghcr.io/haepapa/filearchiver:latest
  - See docker-compose.example.yml for usage examples

## Quick start
- One-off run:
  - ./filearchiver -input /path/to/src -output /path/to/dst
- Using a config file:
  - ./filearchiver -config /path/to/config.yaml
- Initialize existing archive:
  - ./filearchiver -init -output /path/to/existing/archive

### Flags
- -input: source directory for a one-off job
- -output: destination directory for a one-off job
- -config: path to YAML config file (batch jobs)
- -ignorefile: path to a global .archiveignore file applied to all jobs
- -init: initialize mode - scan and register existing files in output directory (requires -output)

### YAML config example
jobs:
  - name: "Photos Archive"
    source: "/path/to/raw"
    destination: "/path/to/archive"
  - name: "Documents Archive"
    source: "/users/docs"
    destination: "/nas/docs"

### Ignore patterns
- Local: place .archiveignore in the source directory
- Global: pass -ignorefile /path/to/.archiveignore
- Supports simple patterns like *.tmp, .DS_Store, node_modules/

## How it works
- Destination path: {destination}/{extension}/{YYYY}/{MM}/{DD}/{filename}
- Collisions: if a file exists, it is moved under {destination}/_duplicates/...; if still colliding, a suffix _01.._99 is added
- Verification: data is copied and hashed; source is removed only after successful verification
- Database: filearchiver.db is created in the working directory with tables history and file_registry
- Safety: a .filearchiver.lock file prevents concurrent runs; remove it if a previous run crashed
- Validation: destination cannot be inside source

### Initialize mode (-init)
Use this mode when you have an existing archive directory or need to rebuild the database:
- Backs up any existing database file with a timestamp suffix (e.g., filearchiver.db.20260222_081955)
- Creates a fresh database and populates it from scratch
- Recursively scans the output directory for all files, including those in _duplicates
- Processes files from _duplicates folder first to allow collision handling
- Files already in valid paths ({extension}/{YYYY}/{MM}/{DD}/{filename}) are registered in the database
- Files in invalid paths are carefully moved to the correct location based on their modification date
- Duplicate collision handling applies during the move process
- Useful for:
  - Initial setup when you already have organized files
  - Recovering from database corruption or loss
  - Migrating from a partially organized structure
  - Rebuilding the database registry from existing archives

Example: ./filearchiver -init -output /path/to/archive

## Testing

### Go Tests (Unit/Integration)
```bash
go test ./...
```
Tests the application logic directly by building and running the binary. Fast and runs everywhere.

### Docker Tests (Container Integration)
```bash
./scripts/test-docker.sh
```
Tests the Docker image build and all functionality in containerized mode:
- Image builds successfully
- Help command works
- One-off archive mode
- Init mode with database handling
- Config-based multi-job mode
- Volume persistence

**Requirements:** Docker must be running. Tests are automated in CI/CD.

## Building cross-platform
- Examples:
  - CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/filearchiver-linux-amd64 ./
  - CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/filearchiver-darwin-arm64 ./
  - CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/filearchiver-windows-amd64.exe ./
- CI builds run automatically on pushes to test and prod after tests pass; artifacts are attached to the workflow run.

## Troubleshooting
- “lock file exists”: remove .filearchiver.lock if no other run is active
- Permission errors: ensure read access on source and write access on destination
- Too many duplicates: more than 99 colliding names in _duplicates; adjust filenames or clean duplicates

## Docker Usage

### Quick Start with Docker
```bash
# Pull the latest image
docker pull ghcr.io/haepapa/filearchiver:latest

# Run a one-off archive job (source will be moved to archive)
docker run --rm \
  -v /path/to/source:/data/input \
  -v /path/to/archive:/data/output \
  -v $(pwd)/data:/data \
  ghcr.io/haepapa/filearchiver:latest \
  -input /data/input -output /data/output

# Initialize existing archive
docker run --rm \
  -v /path/to/archive:/data/output \
  -v $(pwd)/data:/data \
  ghcr.io/haepapa/filearchiver:latest \
  -init -output /data/output

# With config file
docker run --rm \
  -v /path/to/config:/config:ro \
  -v /path/to/source1:/data/source1 \
  -v /path/to/source2:/data/source2 \
  -v /path/to/archive:/data/archive \
  -v $(pwd)/data:/data \
  ghcr.io/haepapa/filearchiver:latest \
  -config /config/config.yaml
```

### Using Docker Compose
See `docker-compose.example.yml` for a complete example with volume mounts and configuration options.

```bash
# Copy and customize the example
cp docker-compose.example.yml docker-compose.yml
# Edit paths and settings
nano docker-compose.yml
# Run
docker-compose up
```

### Volume Mounts
- `/data/input` - Source directory for archiving (files will be moved, not copied)
- `/data/output` - Destination/archive directory
- `/config` - Mount config directory here for YAML files (use `/config/config.yaml`)
- `/data` - Persistent volume for database (filearchiver.db) and lock files
- Mount any custom source/destination paths as needed for your use case

### Important Notes
- **Files are moved, not copied** - Source files are deleted after successful archiving
- Mount source as read-write unless using init mode
- Database persists in the `/data` volume between runs
- Config files should be mounted in `/config` directory (not `/data/config`)

### Building Your Own Image
```bash
docker build -t filearchiver:custom .
docker run --rm filearchiver:custom --help
```
