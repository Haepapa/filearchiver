package api_test

import (
"bytes"
"encoding/json"
"fmt"
"net/http"
"net/http/httptest"
"os"
"path/filepath"
"testing"
"time"

"filearchiver/internal/api"
"filearchiver/internal/db"
)

// setupDuplicateServer creates a test server with real archive files:
//   - primary:   archiveRoot/jpg/2024/03/15/photo.jpg
//   - duplicate: archiveRoot/_duplicates/jpg/2024/03/15/photo.jpg  (identical checksum)
func setupDuplicateServer(t *testing.T) (srv *httptest.Server, archiveRoot string, primaryID, dupID int64) {
t.Helper()

archiveRoot = t.TempDir()

primaryDir := filepath.Join(archiveRoot, "jpg", "2024", "03", "15")
if err := os.MkdirAll(primaryDir, 0755); err != nil {
t.Fatal(err)
}
primaryPath := filepath.Join(primaryDir, "photo.jpg")
if err := os.WriteFile(primaryPath, []byte("primary content"), 0644); err != nil {
t.Fatal(err)
}

dupDir := filepath.Join(archiveRoot, "_duplicates", "jpg", "2024", "03", "15")
if err := os.MkdirAll(dupDir, 0755); err != nil {
t.Fatal(err)
}
dupPath := filepath.Join(dupDir, "photo.jpg")
if err := os.WriteFile(dupPath, []byte("primary content"), 0644); err != nil {
t.Fatal(err)
}

f, err := os.CreateTemp("", "fadup-*.db")
if err != nil {
t.Fatal(err)
}
f.Close()

database, err := db.Open(f.Name())
if err != nil {
t.Fatal(err)
}

for _, stmt := range []string{
`CREATE TABLE IF NOT EXISTS history (
id INTEGER PRIMARY KEY AUTOINCREMENT,
timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
job_name TEXT, status TEXT, message TEXT
)`,
`CREATE TABLE IF NOT EXISTS file_registry (
id INTEGER PRIMARY KEY AUTOINCREMENT,
original_path TEXT, archive_path TEXT, file_name TEXT,
size INTEGER, checksum TEXT, mod_time DATETIME
)`,
} {
if _, err := database.Exec(stmt); err != nil {
t.Fatal(err)
}
}
if err := db.Migrate(database); err != nil {
t.Fatal(err)
}

mod := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)

res1, _ := database.Exec(
`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
 VALUES (?, ?, ?, ?, ?, ?)`,
"/src/photo.jpg", primaryPath, "photo.jpg", 15, "checksum_abc", mod,
)
primaryID, _ = res1.LastInsertId()

res2, _ := database.Exec(
`INSERT INTO file_registry (original_path, archive_path, file_name, size, checksum, mod_time)
 VALUES (?, ?, ?, ?, ?, ?)`,
"/src/photo.jpg", dupPath, "photo.jpg", 15, "checksum_abc", mod,
)
dupID, _ = res2.LastInsertId()

cfg := api.Config{
DB:          database,
ArchiveRoot: archiveRoot,
Readonly:    false,
ThumbDir:    t.TempDir(),
}
srv = httptest.NewServer(api.NewRouter(cfg))
t.Cleanup(func() {
srv.Close()
database.Close()
os.Remove(f.Name())
})
return
}

// ──────────────────────────────────────────────────────────────
// GET /api/duplicates
// ──────────────────────────────────────────────────────────────

func TestListDuplicates(t *testing.T) {
srv, _, _, _ := setupDuplicateServer(t)
resp, err := http.Get(srv.URL + "/api/duplicates")
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
t.Errorf("status = %d, want 200", resp.StatusCode)
}
var groups []map[string]interface{}
if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
t.Fatalf("decode: %v", err)
}
if len(groups) == 0 {
t.Error("expected at least one duplicate group")
}
g := groups[0]
if g["file_name"] != "photo.jpg" {
t.Errorf("file_name = %v, want photo.jpg", g["file_name"])
}
if g["primary"] == nil {
t.Error("primary should not be nil")
}
}

// ──────────────────────────────────────────────────────────────
// DELETE /api/files/{id}
// ──────────────────────────────────────────────────────────────

func TestDeleteFile(t *testing.T) {
srv, _, _, dupID := setupDuplicateServer(t)

url := fmt.Sprintf("%s/api/files/%d?confirm=true", srv.URL, dupID)
req, _ := http.NewRequest(http.MethodDelete, url, nil)
resp, err := http.DefaultClient.Do(req)
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
t.Errorf("status = %d, want 200", resp.StatusCode)
}
}

func TestDeleteFile_NoConfirm(t *testing.T) {
srv, _, _, dupID := setupDuplicateServer(t)
url := fmt.Sprintf("%s/api/files/%d", srv.URL, dupID)
req, _ := http.NewRequest(http.MethodDelete, url, nil)
resp, err := http.DefaultClient.Do(req)
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusBadRequest {
t.Errorf("status = %d, want 400", resp.StatusCode)
}
}

func TestDeleteFile_NotFound(t *testing.T) {
srv, _, _, _ := setupDuplicateServer(t)
url := fmt.Sprintf("%s/api/files/99999?confirm=true", srv.URL)
req, _ := http.NewRequest(http.MethodDelete, url, nil)
resp, err := http.DefaultClient.Do(req)
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusNotFound {
t.Errorf("status = %d, want 404", resp.StatusCode)
}
}

func TestDeleteFile_Readonly(t *testing.T) {
srv := setupReadonlyServer(t)
url := fmt.Sprintf("%s/api/files/1?confirm=true", srv.URL)
req, _ := http.NewRequest(http.MethodDelete, url, nil)
resp, err := http.DefaultClient.Do(req)
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusForbidden {
t.Errorf("status = %d, want 403", resp.StatusCode)
}
}

// ──────────────────────────────────────────────────────────────
// POST /api/duplicates/{id}/promote
// ──────────────────────────────────────────────────────────────

func TestPromoteDuplicate(t *testing.T) {
srv, _, primaryID, dupID := setupDuplicateServer(t)

body, _ := json.Marshal(map[string]int64{"primary_id": primaryID})
url := fmt.Sprintf("%s/api/duplicates/%d/promote", srv.URL, dupID)
resp, err := http.Post(url, "application/json", bytes.NewReader(body))
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
t.Errorf("status = %d, want 200", resp.StatusCode)
}
var file map[string]interface{}
json.NewDecoder(resp.Body).Decode(&file)
if file["is_duplicate"] == true {
t.Error("promoted file should no longer be a duplicate")
}
}

func TestPromoteDuplicate_NotDuplicate(t *testing.T) {
srv, _, primaryID, _ := setupDuplicateServer(t)

url := fmt.Sprintf("%s/api/duplicates/%d/promote", srv.URL, primaryID)
resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("{}")))
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusBadRequest {
t.Errorf("status = %d, want 400 for non-duplicate file", resp.StatusCode)
}
}

func TestPromoteDuplicate_NotFound(t *testing.T) {
srv, _, _, _ := setupDuplicateServer(t)
url := fmt.Sprintf("%s/api/duplicates/99999/promote", srv.URL)
resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("{}")))
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusNotFound {
t.Errorf("status = %d, want 404", resp.StatusCode)
}
}

// ──────────────────────────────────────────────────────────────
// POST /api/duplicates/bulk-delete-identical
// ──────────────────────────────────────────────────────────────

func TestBulkDeleteIdentical(t *testing.T) {
srv, _, _, _ := setupDuplicateServer(t)

url := srv.URL + "/api/duplicates/bulk-delete-identical?confirm=true"
resp, err := http.Post(url, "application/json", nil)
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
t.Errorf("status = %d, want 200", resp.StatusCode)
}
var result map[string]interface{}
json.NewDecoder(resp.Body).Decode(&result)
deleted, _ := result["deleted"].(float64)
if deleted < 1 {
t.Errorf("expected deleted ≥ 1, got %v", result["deleted"])
}
}

func TestBulkDeleteIdentical_NoConfirm(t *testing.T) {
srv, _, _, _ := setupDuplicateServer(t)
url := srv.URL + "/api/duplicates/bulk-delete-identical"
resp, err := http.Post(url, "application/json", nil)
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusBadRequest {
t.Errorf("status = %d, want 400", resp.StatusCode)
}
}

func TestBulkDeleteIdentical_Readonly(t *testing.T) {
srv := setupReadonlyServer(t)
url := srv.URL + "/api/duplicates/bulk-delete-identical?confirm=true"
resp, err := http.Post(url, "application/json", nil)
if err != nil {
t.Fatal(err)
}
defer resp.Body.Close()
if resp.StatusCode != http.StatusForbidden {
t.Errorf("status = %d, want 403", resp.StatusCode)
}
}
