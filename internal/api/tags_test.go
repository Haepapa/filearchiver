package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setupTagData inserts a category + two tags into the server's DB via the API,
// returning the category ID and both tag IDs.
func setupTagData(t *testing.T, srv *httptest.Server) (catID, tag1ID, tag2ID int64) {
	t.Helper()

	// Create category
	body, _ := json.Marshal(map[string]string{"name": "TestCat", "color": "#ff0000"})
	resp, err := http.Post(srv.URL+"/api/tag-categories", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var cat map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&cat)
	catID = int64(cat["id"].(float64))

	// Create tag 1
	b1, _ := json.Marshal(map[string]interface{}{"name": "tag-alpha", "category_id": catID})
	r1, _ := http.Post(srv.URL+"/api/tags", "application/json", bytes.NewReader(b1))
	defer r1.Body.Close()
	var t1 map[string]interface{}
	json.NewDecoder(r1.Body).Decode(&t1)
	tag1ID = int64(t1["id"].(float64))

	// Create tag 2
	b2, _ := json.Marshal(map[string]interface{}{"name": "tag-beta", "category_id": catID})
	r2, _ := http.Post(srv.URL+"/api/tags", "application/json", bytes.NewReader(b2))
	defer r2.Body.Close()
	var t2 map[string]interface{}
	json.NewDecoder(r2.Body).Decode(&t2)
	tag2ID = int64(t2["id"].(float64))

	return
}

// ──────────────────────────────────────────────────────────────
// Tag category endpoints
// ──────────────────────────────────────────────────────────────

func TestListTagCategories(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/tag-categories")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var cats []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&cats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Migrate seeds People, Places, Projects.
	if len(cats) < 3 {
		t.Errorf("expected ≥3 categories from seed, got %d", len(cats))
	}
}

func TestCreateTagCategory(t *testing.T) {
	srv := setupTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "Seasons", "color": "#84cc16"})
	resp, err := http.Post(srv.URL+"/api/tag-categories", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}

func TestCreateTagCategory_MissingName(t *testing.T) {
	srv := setupTestServer(t)
	body, _ := json.Marshal(map[string]string{"color": "#000000"})
	resp, err := http.Post(srv.URL+"/api/tag-categories", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUpdateTagCategory(t *testing.T) {
	srv := setupTestServer(t)
	catID, _, _ := setupTagData(t, srv)

	body, _ := json.Marshal(map[string]string{"name": "Renamed", "color": "#0000ff"})
	req, _ := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/api/tag-categories/%d", srv.URL, catID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestDeleteTagCategory(t *testing.T) {
	srv := setupTestServer(t)
	catID, _, _ := setupTagData(t, srv)

	req, _ := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("%s/api/tag-categories/%d", srv.URL, catID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────
// Tag endpoints
// ──────────────────────────────────────────────────────────────

func TestListTags(t *testing.T) {
	srv := setupTestServer(t)
	setupTagData(t, srv)

	resp, err := http.Get(srv.URL + "/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var tags []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tags)
	if len(tags) < 2 {
		t.Errorf("expected ≥2 tags, got %d", len(tags))
	}
}

func TestListTagsByCategory(t *testing.T) {
	srv := setupTestServer(t)
	catID, _, _ := setupTagData(t, srv)

	resp, err := http.Get(fmt.Sprintf("%s/api/tags?category_id=%d", srv.URL, catID))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var tags []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tags)
	if len(tags) != 2 {
		t.Errorf("expected 2 tags for category, got %d", len(tags))
	}
}

func TestCreateTag(t *testing.T) {
	srv := setupTestServer(t)
	catID, _, _ := setupTagData(t, srv)

	body, _ := json.Marshal(map[string]interface{}{"name": "gamma", "category_id": catID})
	resp, err := http.Post(srv.URL+"/api/tags", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}

func TestCreateTag_MissingName(t *testing.T) {
	srv := setupTestServer(t)
	body, _ := json.Marshal(map[string]interface{}{"category_id": 1})
	resp, err := http.Post(srv.URL+"/api/tags", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUpdateTag(t *testing.T) {
	srv := setupTestServer(t)
	_, tag1ID, _ := setupTagData(t, srv)

	body, _ := json.Marshal(map[string]interface{}{"name": "renamed-alpha"})
	req, _ := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/api/tags/%d", srv.URL, tag1ID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestDeleteTag(t *testing.T) {
	srv := setupTestServer(t)
	_, tag1ID, _ := setupTagData(t, srv)

	req, _ := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("%s/api/tags/%d", srv.URL, tag1ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestMergeTag(t *testing.T) {
	srv := setupTestServer(t)
	_, tag1ID, tag2ID := setupTagData(t, srv)

	body, _ := json.Marshal(map[string]int64{"into_id": tag2ID})
	resp, err := http.Post(
		fmt.Sprintf("%s/api/tags/%d/merge", srv.URL, tag1ID),
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestMergeTag_SameID(t *testing.T) {
	srv := setupTestServer(t)
	_, tag1ID, _ := setupTagData(t, srv)

	body, _ := json.Marshal(map[string]int64{"into_id": tag1ID})
	resp, err := http.Post(
		fmt.Sprintf("%s/api/tags/%d/merge", srv.URL, tag1ID),
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for self-merge", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────
// File-tag endpoints
// ──────────────────────────────────────────────────────────────

func TestGetFileTags_Empty(t *testing.T) {
	srv := setupTestServer(t)
	resp, err := http.Get(srv.URL + "/api/files/1/tags")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var tags []interface{}
	json.NewDecoder(resp.Body).Decode(&tags)
	if tags == nil {
		t.Error("should return empty array not null")
	}
}

func TestSetAndGetFileTags(t *testing.T) {
	srv := setupTestServer(t)
	_, tag1ID, tag2ID := setupTagData(t, srv)

	// Set tags for file 1
	body, _ := json.Marshal(map[string][]int64{"tag_ids": {tag1ID, tag2ID}})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/files/1/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("PUT /api/files/1/tags status = %d, want 200", resp.StatusCode)
	}

	var tags []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&tags)
	if len(tags) != 2 {
		t.Errorf("response tag count = %d, want 2", len(tags))
	}

	// GET to confirm persistence
	resp2, _ := http.Get(srv.URL + "/api/files/1/tags")
	defer resp2.Body.Close()
	var tags2 []map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&tags2)
	if len(tags2) != 2 {
		t.Errorf("GET after PUT: tag count = %d, want 2", len(tags2))
	}
}

func TestSetFileTags_FileNotFound(t *testing.T) {
	srv := setupTestServer(t)
	body, _ := json.Marshal(map[string][]int64{"tag_ids": {}})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/files/99999/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────
// Read-only guard
// ──────────────────────────────────────────────────────────────

func TestReadonlyGuard(t *testing.T) {
	// Spin up a read-only server.
	roSrv := setupReadonlyServer(t)

	writeCases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost,   "/api/tag-categories",             `{"name":"x"}`},
		{http.MethodPatch,  "/api/tag-categories/1",           `{"name":"y"}`},
		{http.MethodDelete, "/api/tag-categories/1",           ""},
		{http.MethodPost,   "/api/tags",                       `{"name":"x"}`},
		{http.MethodPatch,  "/api/tags/1",                     `{"name":"y"}`},
		{http.MethodDelete, "/api/tags/1",                     ""},
		{http.MethodPost,   "/api/tags/1/merge",               `{"into_id":2}`},
		{http.MethodPut,    "/api/files/1/tags",               `{"tag_ids":[]}`},
	}

	for _, tc := range writeCases {
		var bodyReader *bytes.Reader
		if tc.body != "" {
			bodyReader = bytes.NewReader([]byte(tc.body))
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(tc.method, roSrv.URL+tc.path, bodyReader)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("%s %s: %v", tc.method, tc.path, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
}
