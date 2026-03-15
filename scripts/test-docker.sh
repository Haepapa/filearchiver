#!/bin/bash

# Docker Testing Script for filearchiver
# Tests Docker image build and all application modes in the logical order a user
# would encounter them: setup → archive → init.
# Usage: ./scripts/test-docker.sh

# No set -e: failures are tracked via PASSED/FAILED counters so every test runs.

# ── Colours ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ── Configuration ──────────────────────────────────────────────────────────────
IMAGE_NAME="filearchiver:test"
TEST_DIR="/tmp/filearchiver-docker-test-$$"
PASSED=0
FAILED=0
# Run containers as the current host user so bind-mounted directories (owned by
# the host user) are writable by the container.
DOCKER_USER="--user $(id -u):$(id -g)"

# ── Helpers ────────────────────────────────────────────────────────────────────
log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $1"; PASSED=$((PASSED + 1)); }
log_error()   { echo -e "${RED}[FAIL]${NC} $1"; FAILED=$((FAILED + 1)); }
log_warning() { echo -e "${YELLOW}[WARN]${NC} $1"; }

cleanup() {
    log_info "Cleaning up test environment..."
    rm -rf "$TEST_DIR"
    docker rmi "$IMAGE_NAME" 2>/dev/null || true
}
trap cleanup EXIT

# ── Infrastructure ─────────────────────────────────────────────────────────────
log_info "Checking Docker availability..."
if ! command -v docker &>/dev/null; then
    log_error "Docker is not installed or not in PATH"
    exit 1
fi
if ! docker info &>/dev/null; then
    log_error "Docker daemon is not running"
    exit 1
fi
log_success "Docker is available"

log_info "Building Docker image..."
if docker build -t "$IMAGE_NAME" . >/tmp/docker-build.log 2>&1; then
    log_success "Docker image built successfully"
else
    log_error "Docker build failed"
    cat /tmp/docker-build.log
    exit 1
fi
log_info "Image size: $(docker images "$IMAGE_NAME" --format '{{.Size}}')"

mkdir -p "$TEST_DIR"

# ══════════════════════════════════════════════════════════════════════════════
# Test 1: Help command
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 1: Help command..."
if docker run --rm $DOCKER_USER "$IMAGE_NAME" --help >/tmp/help.txt 2>&1; then
    if grep -q "Usage of" /tmp/help.txt; then
        log_success "Test 1: Help command works"
    else
        log_error "Test 1: Help output missing expected text"
    fi
else
    log_error "Test 1: Help command failed"
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 2: Setup mode – config and ignore files created at specified path
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 2: Setup – files created at specified path..."
SETUP_DIR="$TEST_DIR/setup"
mkdir -p "$SETUP_DIR"

if docker run --rm $DOCKER_USER \
    -v "$SETUP_DIR:/workspace" \
    "$IMAGE_NAME" \
    -setup /workspace/conf >/tmp/setup.txt 2>&1; then

    if [ -f "$SETUP_DIR/conf/config.yaml" ]; then
        log_success "Test 2a: config.yaml created inside setup path"
    else
        log_error "Test 2a: config.yaml missing from setup path"
        cat /tmp/setup.txt
    fi

    if [ -f "$SETUP_DIR/conf/.archiveignore" ]; then
        log_success "Test 2b: .archiveignore created inside setup path"
    else
        log_error "Test 2b: .archiveignore missing from setup path"
    fi

    if [ ! -f "$SETUP_DIR/config.yaml" ]; then
        log_success "Test 2c: config.yaml not written to working directory (correct)"
    else
        log_error "Test 2c: config.yaml incorrectly written to working directory"
    fi
else
    log_error "Test 2: Setup command failed"
    cat /tmp/setup.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 3: Setup mode – input and output directories created
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 3: Setup – creates input/output directories..."
SETUP2_DIR="$TEST_DIR/setup2"
mkdir -p "$SETUP2_DIR"

if docker run --rm $DOCKER_USER \
    -v "$SETUP2_DIR:/workspace" \
    "$IMAGE_NAME" \
    -setup /workspace/conf -input /workspace/source -output /workspace/archive >/tmp/setup2.txt 2>&1; then

    if [ -d "$SETUP2_DIR/source" ]; then
        log_success "Test 3a: Input directory created"
    else
        log_error "Test 3a: Input directory not created"
    fi

    if [ -d "$SETUP2_DIR/archive" ]; then
        log_success "Test 3b: Output directory created"
    else
        log_error "Test 3b: Output directory not created"
    fi
else
    log_error "Test 3: Setup with directories failed"
    cat /tmp/setup2.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 4: Setup mode – does not overwrite existing files
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 4: Setup – does not overwrite existing files..."
SETUP3_DIR="$TEST_DIR/setup3"
mkdir -p "$SETUP3_DIR/conf"
printf '%s' "# original config" > "$SETUP3_DIR/conf/config.yaml"
printf '%s' "# original ignore" > "$SETUP3_DIR/conf/.archiveignore"

if docker run --rm $DOCKER_USER \
    -v "$SETUP3_DIR:/workspace" \
    "$IMAGE_NAME" \
    -setup /workspace/conf >/tmp/setup3.txt 2>&1; then

    CONFIG_CONTENT=$(cat "$SETUP3_DIR/conf/config.yaml")
    if [ "$CONFIG_CONTENT" = "# original config" ]; then
        log_success "Test 4a: config.yaml not overwritten"
    else
        log_error "Test 4a: config.yaml was overwritten"
    fi

    IGNORE_CONTENT=$(cat "$SETUP3_DIR/conf/.archiveignore")
    if [ "$IGNORE_CONTENT" = "# original ignore" ]; then
        log_success "Test 4b: .archiveignore not overwritten"
    else
        log_error "Test 4b: .archiveignore was overwritten"
    fi
else
    log_error "Test 4: Setup command failed"
    cat /tmp/setup3.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 5: One-off archive mode
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 5: One-off archive mode..."
ARCH_DIR="$TEST_DIR/oneoff"
mkdir -p "$ARCH_DIR"/{source,archive,config}
echo "text content" > "$ARCH_DIR/source/document.txt"
echo "image data"   > "$ARCH_DIR/source/photo.jpg"
echo "pdf data"     > "$ARCH_DIR/source/report.pdf"

if docker run --rm $DOCKER_USER \
    -v "$ARCH_DIR/source:/data/input" \
    -v "$ARCH_DIR/archive:/data/output" \
    -v "$ARCH_DIR/config:/config" \
    "$IMAGE_NAME" \
    -input /data/input -output /data/output >/tmp/oneoff.txt 2>&1; then

    if [ -z "$(ls -A "$ARCH_DIR/source")" ]; then
        log_success "Test 5a: Source directory emptied after archive"
    else
        log_error "Test 5a: Source not empty after archive"
    fi

    NFILES=$(find "$ARCH_DIR/archive" -type f | wc -l | tr -d ' ')
    if [ "$NFILES" -eq 3 ]; then
        log_success "Test 5b: All 3 files archived ($NFILES found)"
    else
        log_error "Test 5b: Expected 3 archived files, found $NFILES"
        find "$ARCH_DIR/archive" -type f
    fi

    if [ -f "$ARCH_DIR/config/filearchiver.db" ]; then
        log_success "Test 5c: Database created in /config volume"
    else
        log_error "Test 5c: Database not created"
    fi

    TXT_FILES=$(find "$ARCH_DIR/archive/txt" -name "document.txt" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$TXT_FILES" -eq 1 ]; then
        log_success "Test 5d: Files organised under extension/YYYY/MM/DD structure"
    else
        log_error "Test 5d: Extension-based directory structure missing"
        find "$ARCH_DIR/archive" -type f
    fi
else
    log_error "Test 5: Archive mode failed"
    cat /tmp/oneoff.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 6: Global ignore file (-ignorefile)
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 6: Global ignore file (-ignorefile)..."
IGN_DIR="$TEST_DIR/ignorefile"
mkdir -p "$IGN_DIR"/{source,archive,config,conf}
echo "archive me" > "$IGN_DIR/source/keep.txt"
echo "skip me"    > "$IGN_DIR/source/skip.tmp"
echo "*.tmp"      > "$IGN_DIR/conf/global.archiveignore"

if docker run --rm $DOCKER_USER \
    -v "$IGN_DIR/source:/data/input" \
    -v "$IGN_DIR/archive:/data/output" \
    -v "$IGN_DIR/config:/config" \
    -v "$IGN_DIR/conf:/conf:ro" \
    "$IMAGE_NAME" \
    -input /data/input -output /data/output -ignorefile /conf/global.archiveignore >/tmp/ignorefile.txt 2>&1; then

    if [ ! -f "$IGN_DIR/source/keep.txt" ]; then
        log_success "Test 6a: Non-ignored file was archived"
    else
        log_error "Test 6a: Non-ignored file was not archived"
    fi

    if [ -f "$IGN_DIR/source/skip.tmp" ]; then
        log_success "Test 6b: Ignored (.tmp) file remained in source"
    else
        log_error "Test 6b: Ignored file was incorrectly archived"
    fi
else
    log_error "Test 6: Archive with -ignorefile failed"
    cat /tmp/ignorefile.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 7: Local .archiveignore in source directory
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 7: Local .archiveignore in source directory..."
LIGN_DIR="$TEST_DIR/localignore"
mkdir -p "$LIGN_DIR"/{source,archive,config}
echo "archive me" > "$LIGN_DIR/source/file.csv"
echo "ignore me"  > "$LIGN_DIR/source/thumb.db"
echo "thumb.db"   > "$LIGN_DIR/source/.archiveignore"

if docker run --rm $DOCKER_USER \
    -v "$LIGN_DIR/source:/data/input" \
    -v "$LIGN_DIR/archive:/data/output" \
    -v "$LIGN_DIR/config:/config" \
    "$IMAGE_NAME" \
    -input /data/input -output /data/output >/tmp/localignore.txt 2>&1; then

    if [ ! -f "$LIGN_DIR/source/file.csv" ]; then
        log_success "Test 7a: file.csv was archived"
    else
        log_error "Test 7a: file.csv was not archived"
    fi

    if [ -f "$LIGN_DIR/source/thumb.db" ]; then
        log_success "Test 7b: thumb.db ignored by local .archiveignore and remained"
    else
        log_error "Test 7b: thumb.db was archived despite local .archiveignore"
    fi
else
    log_error "Test 7: Local .archiveignore test failed"
    cat /tmp/localignore.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 8: Config mode (multi-job)
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 8: Config mode (multi-job)..."
CFG_DIR="$TEST_DIR/configmode"
mkdir -p "$CFG_DIR"/{source1,source2,archive,config,conf}
echo "from source1" > "$CFG_DIR/source1/alpha.txt"
echo "from source2" > "$CFG_DIR/source2/beta.csv"

cat > "$CFG_DIR/conf/config.yaml" << 'CFGYAML'
jobs:
  - name: "Job 1"
    source: "/data/source1"
    destination: "/data/archive"
  - name: "Job 2"
    source: "/data/source2"
    destination: "/data/archive"
CFGYAML

if docker run --rm $DOCKER_USER \
    -v "$CFG_DIR/source1:/data/source1" \
    -v "$CFG_DIR/source2:/data/source2" \
    -v "$CFG_DIR/archive:/data/archive" \
    -v "$CFG_DIR/config:/config" \
    -v "$CFG_DIR/conf:/conf:ro" \
    "$IMAGE_NAME" \
    -config /conf/config.yaml >/tmp/configmode.txt 2>&1; then

    if [ -z "$(ls -A "$CFG_DIR/source1")" ] && [ -z "$(ls -A "$CFG_DIR/source2")" ]; then
        log_success "Test 8a: Both source directories emptied"
    else
        log_error "Test 8a: Source directories not empty after config run"
    fi

    ARCHIVED=$(find "$CFG_DIR/archive" -type f | wc -l | tr -d ' ')
    if [ "$ARCHIVED" -eq 2 ]; then
        log_success "Test 8b: Both files archived ($ARCHIVED files)"
    else
        log_error "Test 8b: Expected 2 archived files, found $ARCHIVED"
        find "$CFG_DIR/archive" -type f
    fi

    if [ -f "$CFG_DIR/config/filearchiver.db" ]; then
        log_success "Test 8c: Database created in /config volume"
    else
        log_error "Test 8c: Database not created"
    fi
else
    log_error "Test 8: Config mode failed"
    cat /tmp/configmode.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 9: Collision handling (_duplicates)
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 9: Collision handling (_duplicates)..."
COLL_DIR="$TEST_DIR/collision"
mkdir -p "$COLL_DIR"/{source,config}

# Pre-place a file in the archive at today's dated path so the next archived
# file with the same name collides and must go to _duplicates.
CYEAR=$(date +%Y); CMONTH=$(date +%m); CDAY=$(date +%d)
mkdir -p "$COLL_DIR/archive/txt/$CYEAR/$CMONTH/$CDAY"
echo "existing"  > "$COLL_DIR/archive/txt/$CYEAR/$CMONTH/$CDAY/clash.txt"
echo "new file"  > "$COLL_DIR/source/clash.txt"
touch "$COLL_DIR/source/clash.txt"   # ensure mod time == today

if docker run --rm $DOCKER_USER \
    -v "$COLL_DIR/source:/data/input" \
    -v "$COLL_DIR/archive:/data/output" \
    -v "$COLL_DIR/config:/config" \
    "$IMAGE_NAME" \
    -input /data/input -output /data/output >/tmp/collision.txt 2>&1; then

    ORIG=$(find "$COLL_DIR/archive/txt" -name "clash.txt" 2>/dev/null | wc -l | tr -d ' ')
    DUP=$(find "$COLL_DIR/archive/_duplicates" -name "clash.txt" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$ORIG" -ge 1 ] && [ "$DUP" -ge 1 ]; then
        log_success "Test 9: Collision handled – original kept, duplicate in _duplicates"
    else
        log_error "Test 9: Collision not handled correctly (orig=$ORIG dup=$DUP)"
        find "$COLL_DIR/archive" -type f
    fi
else
    log_error "Test 9: Collision test failed"
    cat /tmp/collision.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 10: Init mode with -output – valid files stay, misplaced files move
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 10: Init mode with -output..."
INIT_DIR="$TEST_DIR/init"
mkdir -p "$INIT_DIR"/{archive,config}
mkdir -p "$INIT_DIR/archive/pdf/2025/01/15"
echo "organised"  > "$INIT_DIR/archive/pdf/2025/01/15/report.pdf"
echo "misplaced"  > "$INIT_DIR/archive/stray.txt"

if docker run --rm $DOCKER_USER \
    -v "$INIT_DIR/archive:/data/output" \
    -v "$INIT_DIR/config:/config" \
    "$IMAGE_NAME" \
    -init -output /data/output >/tmp/init.txt 2>&1; then

    if [ -f "$INIT_DIR/archive/pdf/2025/01/15/report.pdf" ]; then
        log_success "Test 10a: Valid file remained at correct path"
    else
        log_error "Test 10a: Valid file was incorrectly moved"
    fi

    if [ ! -f "$INIT_DIR/archive/stray.txt" ]; then
        log_success "Test 10b: Misplaced file was reorganised"
    else
        log_error "Test 10b: Misplaced file was not moved"
    fi

    if [ -f "$INIT_DIR/config/filearchiver.db" ]; then
        log_success "Test 10c: Database created in /config volume"
    else
        log_error "Test 10c: Database not created"
    fi
else
    log_error "Test 10: Init mode failed"
    cat /tmp/init.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 11: Init mode – backs up existing database
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 11: Init mode – backs up existing database..."
INITBK_DIR="$TEST_DIR/initbackup"
mkdir -p "$INITBK_DIR"/{config,archive/txt/2024/01/01}
echo "existing" > "$INITBK_DIR/archive/txt/2024/01/01/old.txt"
touch "$INITBK_DIR/config/filearchiver.db"   # pre-existing database

if docker run --rm $DOCKER_USER \
    -v "$INITBK_DIR/archive:/data/output" \
    -v "$INITBK_DIR/config:/config" \
    "$IMAGE_NAME" \
    -init -output /data/output >/tmp/initbackup.txt 2>&1; then

    BACKUP_COUNT=$(find "$INITBK_DIR/config" -name "filearchiver.db.*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$BACKUP_COUNT" -ge 1 ]; then
        log_success "Test 11a: Existing database backed up ($BACKUP_COUNT backup found)"
    else
        log_error "Test 11a: Database backup not created"
        ls -la "$INITBK_DIR/config/"
    fi

    if [ -f "$INITBK_DIR/config/filearchiver.db" ]; then
        log_success "Test 11b: Fresh database created after backup"
    else
        log_error "Test 11b: Fresh database not created"
    fi
else
    log_error "Test 11: Init with backup test failed"
    cat /tmp/initbackup.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 12: Init mode with -config
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 12: Init mode with -config..."
INITCFG_DIR="$TEST_DIR/initcfg"
# Archive dirs live inside the /data mount; DB goes to the separate /config mount.
mkdir -p "$INITCFG_DIR/data/archive1"
mkdir -p "$INITCFG_DIR/data/archive2"
mkdir -p "$INITCFG_DIR"/{conf,config}
echo "stray in arch1" > "$INITCFG_DIR/data/archive1/stray.jpg"
echo "stray in arch2" > "$INITCFG_DIR/data/archive2/stray.png"

cat > "$INITCFG_DIR/conf/config.yaml" << 'INITCFGYAML'
jobs:
  - name: "Archive1"
    source: "/dev/null"
    destination: "/data/archive1"
  - name: "Archive2"
    source: "/dev/null"
    destination: "/data/archive2"
INITCFGYAML

if docker run --rm $DOCKER_USER \
    -v "$INITCFG_DIR/data:/data" \
    -v "$INITCFG_DIR/config:/config" \
    -v "$INITCFG_DIR/conf:/conf:ro" \
    "$IMAGE_NAME" \
    -init -config /conf/config.yaml >/tmp/initcfg.txt 2>&1; then

    if [ ! -f "$INITCFG_DIR/data/archive1/stray.jpg" ]; then
        log_success "Test 12a: Stray file in archive1 was reorganised"
    else
        log_error "Test 12a: Stray file in archive1 was not moved"
        cat /tmp/initcfg.txt
    fi

    if [ ! -f "$INITCFG_DIR/data/archive2/stray.png" ]; then
        log_success "Test 12b: Stray file in archive2 was reorganised"
    else
        log_error "Test 12b: Stray file in archive2 was not moved"
    fi
else
    log_error "Test 12: Init with -config failed"
    cat /tmp/initcfg.txt
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 13: Init mode – fails when neither -output nor -config is provided
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 13: Init mode error – requires -output or -config..."
INITERR_DIR="$TEST_DIR/initerr"
mkdir -p "$INITERR_DIR/config"

INIT_ERR_OUT=$(docker run --rm $DOCKER_USER \
    -v "$INITERR_DIR/config:/config" \
    "$IMAGE_NAME" \
    -init 2>&1 || true)

if echo "$INIT_ERR_OUT" | grep -q "requires -output flag or -config flag"; then
    log_success "Test 13: Init correctly errors without -output or -config"
else
    log_error "Test 13: Expected error message not found; got: $INIT_ERR_OUT"
fi

# ══════════════════════════════════════════════════════════════════════════════
# Test 14: Volume persistence – databases survive container exit
# ══════════════════════════════════════════════════════════════════════════════
log_info "Test 14: Volume persistence across container runs..."
DB_TOTAL=$(find "$TEST_DIR" -name "filearchiver.db" 2>/dev/null | wc -l | tr -d ' ')
if [ "$DB_TOTAL" -ge 7 ]; then
    log_success "Test 14: Database files persist across runs ($DB_TOTAL found)"
else
    log_error "Test 14: Fewer DB files than expected (found $DB_TOTAL, need >=7)"
    find "$TEST_DIR" -name "filearchiver.db" 2>/dev/null
fi

# ══════════════════════════════════════════════════════════════════════════════
# Summary
# ══════════════════════════════════════════════════════════════════════════════
echo ""
echo "========================================"
echo "         TEST SUMMARY"
echo "========================================"
echo -e "${GREEN}Passed: $PASSED${NC}"
echo -e "${RED}Failed: $FAILED${NC}"
echo "========================================"

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}All tests passed! ✓${NC}"
    exit 0
else
    echo -e "${RED}Some tests failed! ✗${NC}"
    exit 1
fi
