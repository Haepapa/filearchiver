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
- Setup mode to prepare directories and configuration files

## Install
- Prebuilt binaries: After a branch is merged into test or prod, go to GitHub → Actions → “Build binaries” → select the latest run → download artifacts for your OS/arch (filearchiver-<os>-<arch>). Windows binaries have .exe.
- Build from source: Requires Go 1.21+
  - git clone <this repo>
  - go build -o filearchiver ./
- Docker: Pull the container image
  - Test environment: `docker pull ghcr.io/haepapa/filearchiver:test`
  - Production: `docker pull ghcr.io/haepapa/filearchiver:prod` or `ghcr.io/haepapa/filearchiver:latest`
  - See docker-compose.example.yml for usage examples
  - Images are built via GitHub Actions from test/prod branches

## Quick start
- Setup (first time):
  - ./filearchiver -setup /conf/config -input /path/to/src -output /path/to/dst
- One-off run:
  - ./filearchiver -input /path/to/src -output /path/to/dst
- Using a config file:
  - ./filearchiver -config /path/to/config.yaml
- Initialize existing archive (using -output):
  - ./filearchiver -init -output /path/to/existing/archive
- Initialize existing archive (using config file):
  - ./filearchiver -init -config /path/to/config.yaml

### Flags
- -setup: setup mode - create config.yaml and .archiveignore at the given path; also creates -input and -output directories if specified (see Setup mode below)
- -input: source directory for a one-off job
- -output: destination directory for a one-off job
- -config: path to YAML config file (batch jobs)
- -ignorefile: path to a global .archiveignore file applied to all jobs
- -init: initialize mode - scan and register existing files in output directory (requires -output or -config)

### Configuration File

The application supports YAML configuration files for running multiple archive jobs in a single execution. This is useful for automating recurring archive tasks or managing multiple source-destination pairs.

#### Usage
```bash
./filearchiver -config /path/to/config.yaml
```

#### Configuration File Format

Create a YAML file with the following structure:

```yaml
jobs:
  - name: "Photos Archive"
    source: "/path/to/raw"
    destination: "/path/to/archive"
  - name: "Documents Archive"
    source: "/users/docs"
    destination: "/nas/docs"
  - name: "Backup Job"
    source: "/home/user/downloads"
    destination: "/backup/downloads"
```

#### Configuration Keys

**Top-level:**
- `jobs` (required): Array of job configurations

**Job object keys:**
- `name` (required, string): A descriptive name for the job, used in logs and history
- `source` (required, string): Absolute or relative path to the source directory containing files to archive
- `destination` (required, string): Absolute or relative path to the archive destination directory

#### Notes
- All jobs in the config file are processed sequentially
- Each job follows the same archiving rules (extension/date organization, collision handling, verification)
- Job execution continues even if one job fails; check logs for individual job status
- The same ignore patterns (from `-ignorefile` or `.archiveignore`) apply to all jobs
- Both source and destination paths are validated before processing each job

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

### Setup mode (-setup \<path\>)
Use this mode for first-time setup or to prepare the environment. The required `<path>` argument is the directory where `config.yaml` and `.archiveignore` are created:
- Creates `config.yaml` and `.archiveignore` inside the specified path
- The specified path is created automatically if it does not exist
- Creates input and output directories if specified with -input and -output flags (if they don't exist)
- The -config flag overrides the config file location when an explicit path is preferred
- Does not overwrite existing files or directories
- Useful for:
  - First-time setup before running in Docker
  - Keeping configuration files separate from the working directory
  - Creating volume mount points for Docker containers
  - Setting up the environment to add custom ignore patterns before running -init

Example: ./filearchiver -setup /conf/config -input /data/input -output /data/output

**Docker users:** Run this once to create the volume directories and files, then edit config.yaml and .archiveignore before running your archive jobs:
```bash
docker run --rm --user "$(id -u):$(id -g)" -v $(pwd)/config:/config ghcr.io/haepapa/filearchiver:latest -setup /config -input /data/input -output /data/output
# Edit $(pwd)/config/config.yaml and $(pwd)/config/.archiveignore as needed
docker run --rm --user "$(id -u):$(id -g)" -v /source:/data/input -v /archive:/data/output -v $(pwd)/config:/config ghcr.io/haepapa/filearchiver:latest -input /data/input -output /data/output
```

### Initialize mode (-init)
Use this mode when you have an existing archive directory or need to rebuild the database:
- Backs up any existing database file with a timestamp suffix (e.g., filearchiver.db.20260222_081955)
- Creates a fresh database and populates it from scratch
- Recursively scans the output directory for all files, including those in _duplicates
- Processes files from _duplicates folder first to allow collision handling
- Files already in valid paths ({extension}/{YYYY}/{MM}/{DD}/{filename}) are registered in the database
- Files in invalid paths are carefully moved to the correct location based on their modification date
- Duplicate collision handling applies during the move process
- Accepts either -output (single directory) or -config (runs init for every job's destination)
- Useful for:
  - Initial setup when you already have organized files
  - Recovering from database corruption or loss
  - Migrating from a partially organized structure
  - Rebuilding the database registry from existing archives

Examples:
  - Single directory: ./filearchiver -init -output /path/to/archive
  - All destinations in config: ./filearchiver -init -config /path/to/config.yaml

## Testing

### Go Tests (Unit/Integration)
```bash
go test ./...
go test -v ./...
```
Tests the application logic directly by building and running the binary. Fast and runs everywhere.

#### Go test list
| Test name | What it covers |
|---|---|
| TestOneOffRun | Archives files with `-input`/`-output`; source emptied, files in correct paths |
| TestRunUsingConfig | Archives files via `-config` YAML; source emptied, files archived |
| TestRunWithIgnore | Local `.archiveignore` in source prevents ignored files being archived |
| TestInitModeWithValidPaths | `-init -output`: files already in valid paths are registered and left in place |
| TestInitModeWithInvalidPaths | `-init -output`: files at invalid paths are moved to correct location |
| TestInitModeWithMixedPaths | `-init -output`: valid files stay, invalid files are reorganised |
| TestInitModeWithoutOutputOrConfigFlag | `-init` without `-output` or `-config` exits with correct error message |
| TestInitModeWithConfig | `-init -config`: destinations from config file are initialised |
| TestInitModeWithConfigMultipleJobs | `-init -config`: multiple job destinations each initialised |
| TestInitModeWithConfigFileMissing | `-init -config` with missing file exits with error |
| TestInitModeOutputFlagStillWorks | `-init -output` still works after config-based init was added |
| TestInitModeWithNonExistentOutput | `-init -output` with non-existent directory exits with error |
| TestInitModeSkipsDuplicatesFolder | Files inside `_duplicates` are moved to valid paths during init |
| TestInitModeDoesNotAffectNormalOperation | Normal archive runs correctly after init has been used |
| TestInitModeBackupsExistingDatabase | Existing database is backed up with timestamp before init recreates it |
| TestInitModeProcessesDuplicatesFirst | `_duplicates` files are processed before regular files to allow correct collision handling |
| TestInitModeHandlesMultipleDuplicateCollisions | Multiple duplicate files receive correct `_01`, `_02`… suffixes |
| TestInitModeHonorsIgnoreFile | `-ignorefile` patterns respected during init; ignored files are left untouched |
| TestSetupModeCreatesDirectories | `-setup . -input … -output …` creates input and output directories |
| TestSetupModeCreatesConfigFile | `-setup .` creates `config.yaml` in the setup path |
| TestSetupModeCreatesIgnoreFile | `-setup .` creates `.archiveignore` in the setup path |
| TestSetupModeWithCustomConfigPath | `-setup . -config /path` writes config to the explicit path |
| TestSetupModeDoesNotOverwriteExisting | Existing `config.yaml` and `.archiveignore` are never overwritten |
| TestSetupModeDoesNotImpactNormalOperation | Normal archive run works correctly after setup |
| TestSetupModeCreatesFilesInSpecifiedPath | `-setup /conf/config` writes both files inside that directory |
| TestSetupModeCreatesSetupDirectory | Setup path is auto-created when it does not exist |
| TestSetupModePathDoesNotWriteToWorkingDir | When a path is given, files are not written to the working directory |
| TestSetupModePathWithExplicitConfig | `-setup /path -config /other` writes config to explicit path; ignore to setup path |

### Docker Tests (Container Integration)
```bash
./scripts/test-docker.sh
```
Tests the Docker image build and all functionality in containerised mode, following the natural user workflow (setup → archive → init).

**Requirements:** Docker must be running.

#### Docker test list
| Test | What it covers |
|---|---|
| Infrastructure | Docker daemon available; image builds successfully |
| Test 1 | Help command outputs usage text |
| Test 2a | `-setup /path` creates `config.yaml` inside the specified directory |
| Test 2b | `-setup /path` creates `.archiveignore` inside the specified directory |
| Test 2c | `-setup /path` does not write files to the container working directory |
| Test 3a | `-setup /path -input …` creates the input directory |
| Test 3b | `-setup /path -output …` creates the output directory |
| Test 4a | Setup does not overwrite an existing `config.yaml` |
| Test 4b | Setup does not overwrite an existing `.archiveignore` |
| Test 5a | One-off archive (`-input`/`-output`): source directory emptied |
| Test 5b | One-off archive: all files present in archive |
| Test 5c | One-off archive: database created in `/config` volume |
| Test 5d | One-off archive: files organised under `extension/YYYY/MM/DD/` structure |
| Test 6a | `-ignorefile`: non-ignored file is archived |
| Test 6b | `-ignorefile`: ignored (`.tmp`) file remains in source |
| Test 7a | Local `.archiveignore` in source: non-ignored file is archived |
| Test 7b | Local `.archiveignore` in source: ignored file remains |
| Test 8a | Config mode (multi-job): both source directories emptied |
| Test 8b | Config mode: files from all jobs present in archive |
| Test 8c | Config mode: database created |
| Test 9 | Collision handling: original file kept, duplicate moved to `_duplicates` |
| Test 10a | `-init -output`: file already in valid path is left in place |
| Test 10b | `-init -output`: misplaced file is reorganised |
| Test 10c | `-init -output`: database created |
| Test 11a | Init backs up existing database with timestamp suffix |
| Test 11b | Init creates a fresh database after backup |
| Test 12a | `-init -config`: stray file in first destination reorganised |
| Test 12b | `-init -config`: stray file in second destination reorganised |
| Test 13 | `-init` without `-output` or `-config` exits with correct error message |
| Test 14 | Database files persist on the host volume across container runs |



## Building cross-platform
- Examples:
  - CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/filearchiver-linux-amd64 ./
  - CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/filearchiver-darwin-arm64 ./
  - CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/filearchiver-windows-amd64.exe ./
- CI builds run automatically on pushes to test and prod after tests pass; artifacts are attached to the workflow run.

## Building Docker Images

Docker images are built manually via GitHub Actions:

1. Go to: [Actions → Docker Build and Publish](https://github.com/Haepapa/filearchiver/actions)
2. Click "Run workflow"
3. Select environment:
   - **test** - Builds from test branch, tags as `ghcr.io/haepapa/filearchiver:test`
   - **prod** - Builds from prod branch, tags as `ghcr.io/haepapa/filearchiver:prod` and `latest`
4. Click "Run workflow"

Images are built for `linux/amd64` and `linux/arm64` platforms and pushed to GitHub Container Registry.
Images are signed with cosign for security.

## Troubleshooting
- “lock file exists”: remove .filearchiver.lock if no other run is active
- Permission errors: ensure read access on source and write access on destination
- Too many duplicates: more than 99 colliding names in _duplicates; adjust filenames or clean duplicates

## Docker Usage

### Quick Start with Docker
```bash
# Pull the latest image
docker pull ghcr.io/haepapa/filearchiver:latest

# First-time setup: create /config directory and template files
docker run --rm --user "$(id -u):$(id -g)" \
  -v $(pwd)/config:/config \
  ghcr.io/haepapa/filearchiver:latest \
  -setup /config -input /data/input -output /data/output

# Edit config.yaml and .archiveignore in $(pwd)/config/ as needed

# Run a one-off archive job (source will be moved to archive)
docker run --rm --user "$(id -u):$(id -g)" \
  -v /path/to/source:/data/input \
  -v /path/to/archive:/data/output \
  -v $(pwd)/config:/config \
  ghcr.io/haepapa/filearchiver:latest \
  -input /data/input -output /data/output

# Initialize existing archive
docker run --rm --user "$(id -u):$(id -g)" \
  -v /path/to/archive:/data/output \
  -v $(pwd)/config:/config \
  ghcr.io/haepapa/filearchiver:latest \
  -init -output /data/output

# With config file (config.yaml lives in the /config volume)
docker run --rm --user "$(id -u):$(id -g)" \
  -v $(pwd)/config:/config \
  -v /path/to/source1:/data/source1 \
  -v /path/to/source2:/data/source2 \
  -v /path/to/archive:/data/archive \
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
- `/config` - **Persistent volume** for the database (`filearchiver.db`), lock file (`.filearchiver.lock`), config file (`config.yaml`), and ignore file (`.archiveignore`). Mount as read-write.
- Mount any custom source/destination paths as needed for your use case

### Important Notes
- **Files are moved, not copied** - Source files are deleted after successful archiving
- Mount source as read-write unless using init mode
- Database and config files persist in the `/config` volume between runs
- **User permissions**: The container runs as a non-root user (`archiver`, UID 100). When using bind mounts, run the container with `--user "$(id -u):$(id -g)"` (or `user: "${UID:-1000}:${GID:-1000}"` in docker-compose) so the container writes files owned by your host user

### Building Your Own Image
```bash
docker build -t filearchiver:custom .
docker run --rm filearchiver:custom --help
```
