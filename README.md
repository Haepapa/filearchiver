# filearchiver

A small, fast CLI that archives files from a source directory into a structured destination, verifies integrity, and records actions in a local SQLite database. Files are organized by extension and modification date, collisions are handled safely, and ignore patterns are supported.

## Features
- One-off runs with flags or batch runs via YAML config
- Integrity verification (MD5) on copy, then source deletion
- Collision handling using _duplicates and numeric suffixes
- Per-run history and file registry stored in filearchiver.db
- Local and global ignore files (.archiveignore)
- Single-instance protection via .filearchiver.lock

## Install
- Prebuilt binaries: After a branch is merged into test or prod, go to GitHub → Actions → “Build binaries” → select the latest run → download artifacts for your OS/arch (filearchiver-<os>-<arch>). Windows binaries have .exe.
- Build from source: Requires Go 1.21+
  - git clone <this repo>
  - go build -o filearchiver ./

## Quick start
- One-off run:
  - ./filearchiver -input /path/to/src -output /path/to/dst
- Using a config file:
  - ./filearchiver -config /path/to/config.yaml

### Flags
- -input: source directory for a one-off job
- -output: destination directory for a one-off job
- -config: path to YAML config file (batch jobs)
- -ignorefile: path to a global .archiveignore file applied to all jobs

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

## Testing
- Run tests: go test ./...

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

## License
See LICENSE.
