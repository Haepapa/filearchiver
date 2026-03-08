#!/bin/bash

# Docker Testing Script for filearchiver
# Tests Docker image build and all application modes
# Usage: ./scripts/test-docker.sh

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
IMAGE_NAME="filearchiver:test"
TEST_DIR="/tmp/filearchiver-docker-test-$$"
PASSED=0
FAILED=0

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    PASSED=$((PASSED + 1))
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1"
    FAILED=$((FAILED + 1))
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

cleanup() {
    log_info "Cleaning up test environment..."
    rm -rf "$TEST_DIR"
    if [ -n "$IMAGE_NAME" ]; then
        docker rmi "$IMAGE_NAME" 2>/dev/null || true
    fi
}

# Set trap after defining cleanup
trap cleanup EXIT

# Check Docker availability
log_info "Checking Docker availability..."
if ! command -v docker &> /dev/null; then
    log_error "Docker is not installed or not in PATH"
    exit 1
fi

if ! docker info &> /dev/null; then
    log_error "Docker daemon is not running"
    exit 1
fi
log_success "Docker is available"

# Build Docker image
log_info "Building Docker image..."
if ! docker build -t "$IMAGE_NAME" . > /tmp/docker-build.log 2>&1; then
    log_error "Docker build failed"
    cat /tmp/docker-build.log
    exit 1
fi
log_success "Docker image built successfully"

# Get image size
IMAGE_SIZE=$(docker images "$IMAGE_NAME" --format "{{.Size}}")
log_info "Image size: $IMAGE_SIZE"

# Test 1: Help command
log_info "Test 1: Running help command..."
if docker run --rm "$IMAGE_NAME" --help > /tmp/help-output.txt 2>&1; then
    if grep -q "Usage of" /tmp/help-output.txt; then
        log_success "Help command works"
    else
        log_error "Help output doesn't contain expected text"
    fi
else
    log_error "Help command failed"
fi

# Create test environment
log_info "Creating test environment..."
mkdir -p "$TEST_DIR"/{source,archive,archive_init,archive_config,data,data_init,data_config,source1,source2,config}

# Test 2: Setup mode
log_info "Test 2: Setup mode..."
SETUP_TEST_DIR="$TEST_DIR/setup_test"
mkdir -p "$SETUP_TEST_DIR"

if docker run --rm \
    -v "$SETUP_TEST_DIR:/data" \
    "$IMAGE_NAME" \
    -setup -input /data/input -output /data/output > /tmp/setup-output.txt 2>&1; then
    
    # Check if directories were created
    if [ -d "$SETUP_TEST_DIR/input" ] && [ -d "$SETUP_TEST_DIR/output" ]; then
        log_success "Setup created input and output directories"
    else
        log_error "Setup did not create directories"
        ls -la "$SETUP_TEST_DIR"
    fi
    
    # Check if config file was created
    if [ -f "$SETUP_TEST_DIR/config.yaml" ]; then
        log_success "Setup created config.yaml"
    else
        log_error "Setup did not create config.yaml"
    fi
    
    # Check if ignore file was created
    if [ -f "$SETUP_TEST_DIR/.archiveignore" ]; then
        log_success "Setup created .archiveignore"
    else
        log_error "Setup did not create .archiveignore"
    fi
else
    log_error "Setup mode failed"
    cat /tmp/setup-output.txt
fi

# Test 3: One-off archive mode
log_info "Test 3: One-off archive mode..."
echo "test file 1" > "$TEST_DIR/source/file1.txt"
echo "test file 2" > "$TEST_DIR/source/file2.jpg"
echo "test file 3" > "$TEST_DIR/source/doc.pdf"

if docker run --rm \
    -v "$TEST_DIR/source:/data/input" \
    -v "$TEST_DIR/archive:/data/output" \
    -v "$TEST_DIR/data:/data" \
    "$IMAGE_NAME" \
    -input /data/input -output /data/output > /tmp/archive-output.txt 2>&1; then
    
    # Check if source is empty
    if [ -z "$(ls -A "$TEST_DIR/source")" ]; then
        log_success "Source directory emptied"
    else
        log_error "Source directory not empty"
    fi
    
    # Check if files are organized correctly
    FOUND_FILES=$(find "$TEST_DIR/archive" -type f | wc -l | tr -d ' ')
    if [ "$FOUND_FILES" -eq 3 ]; then
        log_success "Files organized correctly (found $FOUND_FILES files)"
    else
        log_error "Wrong number of files organized (expected 3, found $FOUND_FILES)"
        find "$TEST_DIR/archive" -type f
    fi
    
    # Check if database was created
    if [ -f "$TEST_DIR/data/filearchiver.db" ]; then
        log_success "Database created"
    else
        log_error "Database not created"
    fi
else
    log_error "Archive mode failed"
    cat /tmp/archive-output.txt
fi

# Test 4: Init mode
log_info "Test 4: Init mode..."
# Create mixed structure
mkdir -p "$TEST_DIR/archive_init/pdf/2025/01/15"
echo "organized" > "$TEST_DIR/archive_init/pdf/2025/01/15/proper.pdf"
echo "unorganized" > "$TEST_DIR/archive_init/messy.txt"
mkdir -p "$TEST_DIR/archive_init/random"
echo "also unorganized" > "$TEST_DIR/archive_init/random/file.jpg"

# Note: Not creating a fake database, let init mode create it fresh

if docker run --rm \
    -v "$TEST_DIR/archive_init:/data/output" \
    -v "$TEST_DIR/data_init:/data" \
    "$IMAGE_NAME" \
    -init -output /data/output > /tmp/init-output.txt 2>&1; then
    
    # Check for new database (backup won't exist if there was no old db)
    if [ -f "$TEST_DIR/data_init/filearchiver.db" ]; then
        log_success "Database created during init"
    else
        log_error "Database not created during init"
    fi
    
    # Check if organized file remained
    if [ -f "$TEST_DIR/archive_init/pdf/2025/01/15/proper.pdf" ]; then
        log_success "Organized file remained in place"
    else
        log_error "Organized file was incorrectly moved"
    fi
    
    # Check if unorganized files were moved (check root for messy.txt)
    if [ ! -f "$TEST_DIR/archive_init/messy.txt" ]; then
        log_success "Unorganized files were moved from root"
    else
        log_error "Unorganized files still in root"
    fi
    
    # Check if they're in correct locations now
    NEW_FILES=$(find "$TEST_DIR/archive_init" -type f | wc -l | tr -d ' ')
    if [ "$NEW_FILES" -eq 3 ]; then
        log_success "All files now in organized structure ($NEW_FILES files total)"
    else
        log_error "Wrong number of files after init (expected 3, found $NEW_FILES)"
        find "$TEST_DIR/archive_init" -type f
    fi
else
    log_error "Init mode failed"
    cat /tmp/init-output.txt
fi

# Test 5: Config mode
log_info "Test 5: Config mode..."
echo "from source1" > "$TEST_DIR/source1/file1.txt"
echo "from source2" > "$TEST_DIR/source2/file2.csv"

cat > "$TEST_DIR/config/config.yaml" << EOF
jobs:
  - name: "Job 1"
    source: "/data/source1"
    destination: "/data/archive"
  - name: "Job 2"
    source: "/data/source2"
    destination: "/data/archive"
EOF

if docker run --rm \
    -v "$TEST_DIR/source1:/data/source1" \
    -v "$TEST_DIR/source2:/data/source2" \
    -v "$TEST_DIR/archive_config:/data/archive" \
    -v "$TEST_DIR/config:/config:ro" \
    -v "$TEST_DIR/data_config:/data" \
    "$IMAGE_NAME" \
    -config /config/config.yaml > /tmp/config-output.txt 2>&1; then
    
    # Check if both sources are empty
    if [ -z "$(ls -A "$TEST_DIR/source1")" ] && [ -z "$(ls -A "$TEST_DIR/source2")" ]; then
        log_success "Both source directories emptied"
    else
        log_error "Source directories not empty"
    fi
    
    # Check if files from both jobs are archived
    ARCHIVED_FILES=$(find "$TEST_DIR/archive_config" -type f | wc -l | tr -d ' ')
    if [ "$ARCHIVED_FILES" -eq 2 ]; then
        log_success "Files from both jobs archived correctly ($ARCHIVED_FILES files)"
    else
        log_error "Wrong number of archived files (expected 2, found $ARCHIVED_FILES)"
        find "$TEST_DIR/archive_config" -type f
    fi
else
    log_error "Config mode failed"
    cat /tmp/config-output.txt
fi

# Test 6: Volume persistence
log_info "Test 6: Volume persistence..."
DB_COUNT=$(ls "$TEST_DIR"/data*/filearchiver.db* 2>/dev/null | wc -l)
if [ "$DB_COUNT" -ge 3 ]; then
    log_success "Database files persist across runs ($DB_COUNT files found)"
else
    log_error "Database persistence issue (only $DB_COUNT files found)"
fi

# Print summary
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
