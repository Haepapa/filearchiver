package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// nav_test.go shares setupTestServer, createSchema, and seedTestData from handler_test.go.

// ---------------------------------------------------------------------------
// GET /api/nav/types
// ---------------------------------------------------------------------------

func TestGetNavTypesEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/nav/types")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Test DB has 3 files: a.jpg, b.mp4, c.jpg → jpg(2) and mp4(1)
	if len(body) < 1 {
		t.Error("expected at least one extension type")
	}
}

func TestGetNavTypesContentType(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/nav/types")
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}

func TestGetNavTypesHasRequiredFields(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/nav/types")
	defer resp.Body.Close()

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) == 0 {
		t.Skip("no files in test db")
	}
	for _, field := range []string{"extension", "count", "size"} {
		if _, ok := body[0][field]; !ok {
			t.Errorf("missing field %q in nav/types response", field)
		}
	}
}

func TestGetNavTypesSortedByCount(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/nav/types")
	defer resp.Body.Close()

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) < 2 {
		t.Skip("need at least 2 extension types to test sort order")
	}
	// First entry should have the highest count.
	c0 := body[0]["count"].(float64)
	c1 := body[1]["count"].(float64)
	if c0 < c1 {
		t.Errorf("expected descending count order: first=%v second=%v", c0, c1)
	}
}

// ---------------------------------------------------------------------------
// GET /api/nav/dates
// ---------------------------------------------------------------------------

func TestGetNavDatesEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/nav/dates")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) == 0 {
		t.Error("expected at least one year entry")
	}
}

func TestGetNavDatesHasRequiredFields(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/nav/dates")
	defer resp.Body.Close()

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) == 0 {
		t.Skip("no date data")
	}
	for _, field := range []string{"year", "count", "months"} {
		if _, ok := body[0][field]; !ok {
			t.Errorf("missing field %q in nav/dates response", field)
		}
	}
}

func TestGetNavDatesMonthsIsArray(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/nav/dates")
	defer resp.Body.Close()

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) == 0 {
		t.Skip("no date data")
	}
	months, ok := body[0]["months"].([]interface{})
	if !ok {
		t.Errorf("expected months to be an array, got %T", body[0]["months"])
	}
	if len(months) == 0 {
		t.Error("expected at least one month entry")
	}
}

// ---------------------------------------------------------------------------
// GET /api/nav/tags
// ---------------------------------------------------------------------------

func TestGetNavTagsEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/nav/tags")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	// Migration seeds 3 default categories.
	if len(body) != 3 {
		t.Errorf("expected 3 default tag categories, got %d", len(body))
	}
}

func TestGetNavTagsHasRequiredFields(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/nav/tags")
	defer resp.Body.Close()

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) == 0 {
		t.Skip("no tag categories")
	}
	for _, field := range []string{"id", "name", "color", "tags"} {
		if _, ok := body[0][field]; !ok {
			t.Errorf("missing field %q in nav/tags response", field)
		}
	}
}

func TestGetNavTagsTagsIsArray(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/nav/tags")
	defer resp.Body.Close()

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) == 0 {
		t.Skip("no categories")
	}
	if _, ok := body[0]["tags"].([]interface{}); !ok {
		t.Errorf("expected tags to be an array, got %T", body[0]["tags"])
	}
}

// ---------------------------------------------------------------------------
// GET /api/history/recent
// ---------------------------------------------------------------------------

func TestGetRecentHistoryEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/history/recent")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	// seedTestData has 2 SUCCESS entries.
	if len(body) != 2 {
		t.Errorf("expected 2 recent SUCCESS entries, got %d", len(body))
	}
	for _, e := range body {
		if e["status"] != "SUCCESS" {
			t.Errorf("expected only SUCCESS entries, got %q", e["status"])
		}
	}
}

func TestGetRecentHistoryResponseFields(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/history/recent")
	defer resp.Body.Close()

	var body []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body) == 0 {
		t.Skip("no history")
	}
	for _, field := range []string{"id", "timestamp", "job_name", "status", "message"} {
		if _, ok := body[0][field]; !ok {
			t.Errorf("missing field %q in history/recent response", field)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /api/files – Year / Month filter (Phase 2 addition)
// ---------------------------------------------------------------------------

func TestListFilesYearFilterEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files?year=2024")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Total float64 `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	// All 3 test files are from 2024.
	if body.Total != 3 {
		t.Errorf("expected 3 files in 2024, got %v", body.Total)
	}
}

func TestListFilesYearMonthFilterEndpoint(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files?year=2024&month=01")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Total float64 `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	// seedTestData: a.jpg=2024-01-15, b.mp4=2024-02-20, c.jpg=2024-03-01 → only 1 in Jan
	if body.Total != 1 {
		t.Errorf("expected 1 file in 2024-01, got %v", body.Total)
	}
}

func TestListFilesNoResultsForMissingYear(t *testing.T) {
	srv := setupTestServer(t)
	resp, _ := http.Get(srv.URL + "/api/files?year=1999")
	defer resp.Body.Close()

	var body struct {
		Total float64 `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 0 {
		t.Errorf("expected 0 files for year 1999, got %v", body.Total)
	}
}
