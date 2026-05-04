# filearchiver Web UI — Feature Specification & Implementation Plan

## 1. Overview

This document specifies a web-based UI (`filearchiver-web`) that sits on top of the existing `filearchiver.db` SQLite database. It provides a browser-based interface for browsing, searching, tagging, and managing the archive. It is a **read-mostly** companion to the CLI archiver — the archiver continues to move and catalogue files; the web UI provides visibility and management on top of that catalogue.

The web UI is a separate binary/service that reads from (and lightly extends) the same `filearchiver.db` produced by the CLI tool. It must run identically when installed locally as a binary or deployed via Docker.

---

## 2. Architecture

### 2.1 Component Layout

```
filearchiver-web          ← new Go binary (same module)
├── cmd/web/main.go       ← entry point, flags, server startup
├── internal/api/         ← REST API handlers
├── internal/db/          ← DB access layer (shared + new tables)
├── internal/media/       ← MIME detection, thumbnail generation
└── web/                  ← embedded static frontend (HTML/CSS/JS)
```

The frontend is **embedded** into the binary via `go:embed` so there is no external asset dependency — one binary, one port.

### 2.2 Tech Stack

| Layer | Choice | Rationale |
|---|---|---|
| Backend language | Go 1.24 | Same module, no new runtime dependency |
| HTTP router | `net/http` + `chi` | Lightweight, no frameworks needed |
| Database | SQLite via `modernc.org/sqlite` | Same driver already in `go.mod` |
| Frontend | HTMX + Alpine.js + Tailwind CSS (CDN) | No build toolchain; works as embedded HTML |
| Media serving | Go `http.ServeContent` with range support | Enables native browser video/audio streaming |
| Thumbnails | Go `imaging` library (optional, Phase 2) | In-process thumbnail generation for images |

### 2.3 Deployment Modes

**Local binary:**
```bash
filearchiver-web -db /path/to/filearchiver.db -archive /path/to/archive -port 8080
```

**Docker (new service alongside existing archiver):**
```yaml
filearchiver-web:
  image: ghcr.io/haepapa/filearchiver-web:latest
  ports:
    - "8080:8080"
  volumes:
    - filearchiver-config:/config      # contains filearchiver.db
    - /path/to/archive:/data/archive   # read access to actual files
  command: ["-db", "/config/filearchiver.db", "-archive", "/data/archive"]
```

Both modes use identical flags and behaviour — no environment-specific code paths.

---

## 3. Database Schema Extensions

The web UI adds new tables to `filearchiver.db`. These are created automatically on first startup if they do not exist, and are never modified by the CLI archiver (additive only, no breaking changes).

### 3.1 New Tables

```sql
-- Tag categories (e.g. People, Places, Projects)
CREATE TABLE IF NOT EXISTS tag_categories (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    name      TEXT    NOT NULL UNIQUE,
    color     TEXT    NOT NULL DEFAULT '#6b7280',  -- hex colour for UI badge
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Seed default categories
INSERT OR IGNORE INTO tag_categories (name, color) VALUES
    ('People',   '#3b82f6'),
    ('Places',   '#10b981'),
    ('Projects', '#f59e0b');

-- Tags
CREATE TABLE IF NOT EXISTS tags (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    category_id INTEGER REFERENCES tag_categories(id) ON DELETE SET NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (name, category_id)
);

-- Many-to-many: file_registry <-> tags
CREATE TABLE IF NOT EXISTS file_tags (
    file_id    INTEGER NOT NULL REFERENCES file_registry(id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (file_id, tag_id)
);

-- Index for performance
CREATE INDEX IF NOT EXISTS idx_file_tags_file  ON file_tags(file_id);
CREATE INDEX IF NOT EXISTS idx_file_tags_tag   ON file_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_registry_ext    ON file_registry(file_name);  -- for ext filtering
CREATE INDEX IF NOT EXISTS idx_registry_path   ON file_registry(archive_path);
```

### 3.2 Existing Tables (read-only from web UI)

| Table | Web UI use |
|---|---|
| `file_registry` | Browse, search, metadata display, media serving |
| `history` | Archive history log, per-file history |

---

## 4. Feature Specifications

### 4.1 Dashboard

**Purpose:** Landing page overview of the archive.

**Elements:**
- **Stats bar:** total file count, total archive size (human-readable), number of unique extensions, number of tagged files
- **File type breakdown:** horizontal bar chart of top 10 extensions by file count and size
- **Recent archives:** table of the last 20 `history` entries with status = SUCCESS, showing filename, job name, and timestamp
- **Tag cloud:** all tags rendered as clickable badges (sized by frequency), clicking navigates to filtered browse view
- **Storage timeline:** monthly bar chart of files archived (derived from `mod_time`)

---

### 4.2 File Browser

**Purpose:** Explore all archived files with flexible organisation.

**View modes:**
- **Grid view** — thumbnail cards (image files show preview, others show extension icon)
- **List view** — table with columns: filename, extension, size, mod date, tags, archive path

**Left-panel navigation tree:**
```
📁 All Files
📁 By Type
  ├── jpg (1,204)
  ├── mp4 (88)
  └── pdf (312)
📁 By Date
  ├── 2024
  │   ├── 01
  │   └── 02
  └── 2025
📁 By Tag
  ├── People
  │   ├── Alice
  │   └── Bob
  ├── Places
  └── Projects
📁 Duplicates
```

**Sorting:** filename, size, mod date, date archived (asc/desc)

**Pagination:** 50 items per page with next/previous; URL reflects current page and filters so links are shareable.

---

### 4.3 Search

**Purpose:** Find files quickly across the entire archive.

**Search scope:** filename, original path, archive path, tags (name), job name

**Filters (combinable):**
- Extension (multi-select checkboxes)
- Date range (mod_time: from / to date pickers)
- Tags (multi-select, AND/OR mode)
- Archive job name
- File size range (min/max bytes)
- In duplicates folder (yes / no / both)

**Behaviour:**
- Incremental search with debounce (300 ms) — no page reload required (HTMX swap)
- Search results show matched fields highlighted
- Filters and search query reflected in URL query params for bookmarking

**API endpoint:** `GET /api/files?q=&ext=&tag=&from=&to=&page=&per_page=`

---

### 4.4 Media Viewer

**Purpose:** View and play archived files without leaving the browser.

**Supported file types:**

| Category | Extensions | Viewer |
|---|---|---|
| Images | jpg, jpeg, png, gif, webp, bmp, tiff, svg, heic | Native `<img>` with pan/zoom (Panzoom.js) |
| Video | mp4, mov, avi, mkv, webm, m4v | Native HTML5 `<video>` with controls; range request support for seeking |
| Audio | mp3, wav, flac, aac, m4a, ogg | Native HTML5 `<audio>` with waveform display |
| PDF | pdf | Browser inline `<embed>` or PDF.js |
| Text/code | txt, md, json, yaml, csv, log, xml | Syntax-highlighted `<pre>` block |
| Other | any | Download link; no preview |

**Viewer panel features:**
- Opens as a modal overlay from the browser (no navigation away)
- Previous / Next navigation within the current filtered/browsed set
- Keyboard shortcuts: `←` / `→` navigate, `Esc` close, `Space` play/pause video/audio, `f` fullscreen
- Download button (triggers browser file download)
- Metadata sidebar (toggleable):
  - Archive path, original path
  - File size (human-readable)
  - MD5 checksum
  - Original modification date
  - Date archived (from `history` table)
  - Archive job name
  - Tags (editable inline)

**File serving endpoint:** `GET /api/file/{id}/content` — streams the file from `archive_path` on disk using `http.ServeContent` (supports `Range` headers for video seeking).

---

### 4.5 Tagging System

**Purpose:** Annotate files with user-defined tags organised into categories.

#### 4.5.1 Tagging a File

- Tags are shown as coloured badges in the viewer metadata sidebar, file cards, and list rows
- **Inline tag editor:** clicking the tag area on a file opens a dropdown with typeahead search over existing tags, plus "Create new tag" if no match found
- Multi-tag support: a file can have any number of tags across multiple categories
- Tag changes are saved immediately (PATCH `/api/files/{id}/tags`)
- Bulk tagging: select multiple files in browse view → "Tag selected" applies chosen tags to all

#### 4.5.2 Tag Management Page (`/tags`)

- List all tags grouped by category with usage count (number of files)
- **Create tag:** name + category (required), displayed as a badge preview
- **Edit tag:** rename or reassign category
- **Delete tag:** confirmation required; removes from all files (cascade)
- **Merge tags:** combine two tags into one (reassigns all file_tags, deletes source tag)
- **Tag categories:** create/rename/delete categories; assign colours

#### 4.5.3 Tag-Based Organisation

- Browse view left panel shows tag tree (category → tag → files)
- Clicking any tag navigates to a filtered browse showing only files with that tag
- Tags shown as filter chips in the active filter bar

---

### 4.6 Duplicate Manager

**Purpose:** Review and clean up files in the `_duplicates` folder.

**Access:** Sidebar "Duplicates" section, or via a badge on any file that has duplicates.

**Duplicate groups view:**
- Files in `_duplicates` are grouped with their primary counterpart (matched by base filename and date path)
- Each group shows:
  - Primary file (in main archive path) — displayed on left
  - Duplicate(s) (in `_duplicates/`) — displayed on right
  - Side-by-side metadata comparison: size, checksum, mod date
  - If checksums match: "Identical" badge; if different: "Different content" warning badge
  - Preview thumbnail/player for both (for visual confirmation)

**Actions per duplicate:**
- **Delete duplicate** — removes the file from disk and from `file_registry`; requires confirmation dialog
- **Delete primary & promote duplicate** — deletes the primary, moves duplicate to the primary path, updates `file_registry`
- **Keep both** — dismisses the group from the "needs review" view without deleting

**Bulk actions:**
- "Delete all identical duplicates" — deletes all `_duplicates` entries where checksum matches the primary; single confirmation for batch

**Safety:**
- All delete operations are irreversible; confirmation modal shows filename and full path before proceeding
- A toast notification confirms each action

---

### 4.7 Archive History Log

**Purpose:** Full audit trail of all CLI archiver runs.

**Page (`/history`):**
- Paginated table of all `history` rows, newest first
- Columns: timestamp, job name, status (colour-coded SUCCESS/FAILED/SKIPPED), message
- Filter by: job name, status, date range
- Summary bar: total operations, success rate, last run time

**Per-file history:**
- In the file viewer metadata sidebar, a "History" tab shows all `history` rows whose `message` contains the file's archive path (or filename)

---

### 4.8 Settings Page (`/settings`)

- **Database info:** path, file size, last modified, table row counts
- **Archive root:** display/confirm the configured archive root path
- **Thumbnail cache:** clear generated thumbnail cache
- **Tag categories:** quick access to create/edit categories (full management at `/tags`)
- **Port / bind address:** display only (set at startup via flags)

---

## 5. REST API Reference

All endpoints return JSON. File content endpoint returns the file's MIME type.

### Files

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/files` | List/search files. Query params: `q`, `ext`, `tag`, `from`, `to`, `job`, `duplicates_only`, `page`, `per_page`, `sort`, `order` |
| `GET` | `/api/files/{id}` | Single file metadata |
| `GET` | `/api/files/{id}/content` | Stream file content (range requests supported) |
| `GET` | `/api/files/{id}/thumbnail` | Thumbnail image (generated on first request, cached) |
| `GET` | `/api/files/{id}/history` | Archive history entries related to this file |
| `PATCH` | `/api/files/{id}/tags` | Set tags for file. Body: `{"tag_ids": [1, 2, 3]}` |
| `DELETE` | `/api/files/{id}` | Delete file from disk and registry |

### Tags

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/tags` | List all tags with usage count |
| `POST` | `/api/tags` | Create tag. Body: `{"name": "", "category_id": 1}` |
| `PATCH` | `/api/tags/{id}` | Rename or reassign category |
| `DELETE` | `/api/tags/{id}` | Delete tag (cascades from file_tags) |
| `POST` | `/api/tags/{id}/merge` | Merge into another tag. Body: `{"target_id": 2}` |

### Tag Categories

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/tag-categories` | List all categories |
| `POST` | `/api/tag-categories` | Create category |
| `PATCH` | `/api/tag-categories/{id}` | Rename / recolour |
| `DELETE` | `/api/tag-categories/{id}` | Delete (tags in this category become uncategorised) |

### History

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/history` | List history. Query params: `job`, `status`, `from`, `to`, `page`, `per_page` |

### Stats

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/stats` | Aggregate stats for the dashboard |
| `GET` | `/api/stats/timeline` | Monthly archive counts for chart |

### Duplicates

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/duplicates` | List duplicate groups |
| `DELETE` | `/api/duplicates/{id}` | Delete the duplicate file (keeps primary) |
| `POST` | `/api/duplicates/{id}/promote` | Promote duplicate to primary, delete primary |

---

## 6. CLI Flags (filearchiver-web binary)

```
-db       string   Path to filearchiver.db (required)
-archive  string   Root path of the archive directory (required for file serving)
-port     int      HTTP port to listen on (default: 8080)
-host     string   Bind address (default: 0.0.0.0)
-readonly bool     Disable all write/delete operations (default: false)
-thumbdir string   Directory to cache thumbnails (default: <db_dir>/.thumbcache)
```

---

## 7. Implementation Plan

### Phase 1 — Project Scaffold & Database Layer

**Goal:** Go project structure, DB migration, and basic server startup.

1. Create `cmd/web/main.go` — flag parsing, DB open, server start
2. Create `internal/db/` — shared DB access (open, migrate), query functions for `file_registry`, `history`
3. Add new tables migration (tag_categories, tags, file_tags, indexes) run on startup
4. `GET /api/stats` endpoint (total files, size, extension breakdown)
5. `GET /api/files` endpoint (list with pagination, basic sort)
6. Wire `chi` router; static file serving from `web/` via `go:embed`
7. Add `filearchiver-web` build target to `Makefile` / CI workflow

### Phase 2 — Frontend Shell & File Browser

**Goal:** Working browser UI for exploring files.

8. HTML shell (`web/index.html`) with Tailwind CDN, HTMX, Alpine.js
9. Dashboard page — stats bar, recent archives table
10. Sidebar navigation tree (by type, by date, by tag, duplicates)
11. File browser page — list and grid views, pagination
12. Navigation tree queries (`GET /api/files?ext=jpg`, `GET /api/files?year=2024`)
13. Sort and filter bar UI

### Phase 3 — Media Viewer

**Goal:** View and play all media types in-browser.

14. `GET /api/files/{id}/content` — file streaming with Range support
15. `GET /api/files/{id}/thumbnail` — image thumbnail generation and cache
16. Modal viewer component (HTMX-driven overlay)
17. Image viewer with pan/zoom
18. HTML5 video and audio players
19. PDF inline embed
20. Text/code viewer with highlighting
21. Keyboard navigation (prev/next/esc/space/f)
22. Metadata sidebar (file info + history tab)

### Phase 4 — Tagging

**Goal:** Full tagging workflow.

23. `GET/POST/PATCH/DELETE /api/tags` and `/api/tag-categories`
24. `PATCH /api/files/{id}/tags`
25. Tag editor component in file viewer sidebar
26. Bulk tag action from browse selection
27. `/tags` management page (list, create, edit, delete, merge)
28. Tag filter in sidebar navigation tree
29. Tag filter chips in active-filter bar

### Phase 5 — Duplicate Manager

**Goal:** Review and resolve duplicate files.

30. Duplicate detection query (files in `_duplicates` path, grouped by basename)
31. `GET /api/duplicates` — grouped duplicate list
32. `DELETE /api/files/{id}` — delete file from disk + registry
33. `POST /api/duplicates/{id}/promote` — promote duplicate
34. Duplicate manager page (`/duplicates`) with side-by-side comparison
35. Bulk "delete identical" action

### Phase 6 — Search & History

**Goal:** Fast full-text search and full history visibility.

36. Full-text search query (SQLite LIKE / FTS5 extension if available)
37. Search API with all filter params
38. Search UI — filter sidebar, result highlighting, URL state
39. `/history` page — full log table with filters
40. Per-file history in viewer sidebar

### Phase 7 — Docker & Deployment

**Goal:** Production-ready Docker image and updated documentation.

41. `Dockerfile.web` — multi-stage build for `filearchiver-web`
42. Update `docker-compose.example.yml` to include `filearchiver-web` service
43. GitHub Actions workflow: build & publish `filearchiver-web` image
44. Update `README.md` with web UI setup instructions
45. Settings page (`/settings`)

---

## 8. Security Considerations

- The web UI has **no authentication** by default — it is intended for local/trusted-network use. For public-facing deployments, place it behind a reverse proxy with basic auth (nginx, Caddy, Traefik).
- The `-readonly` flag disables all DELETE and POST/PATCH write endpoints — safe for shared/read-only deployments.
- File serving is restricted to paths under the configured `-archive` root; no path traversal is possible.
- Delete operations require an explicit confirmation step in the UI; the API requires a `confirm=true` query param as a CSRF-mitigating double-submit guard.

---

## 9. File Structure (target)

```
filearchiver/
├── main.go                        ← existing CLI archiver
├── main_test.go
├── cmd/
│   └── web/
│       └── main.go                ← web UI entry point
├── internal/
│   ├── db/
│   │   ├── db.go                  ← open, migrate, shared queries
│   │   ├── files.go               ← file_registry queries
│   │   ├── tags.go                ← tag/category queries
│   │   ├── history.go             ← history queries
│   │   └── duplicates.go          ← duplicate detection queries
│   ├── api/
│   │   ├── router.go              ← chi router setup
│   │   ├── files.go               ← file handlers
│   │   ├── tags.go                ← tag handlers
│   │   ├── history.go             ← history handlers
│   │   ├── duplicates.go          ← duplicate handlers
│   │   └── stats.go               ← stats/dashboard handlers
│   └── media/
│       ├── serve.go               ← range-aware file serving
│       ├── thumbnail.go           ← thumbnail generation + cache
│       └── mime.go                ← extension → MIME mapping
├── web/
│   ├── index.html                 ← SPA shell
│   ├── components/                ← HTMX partial templates
│   │   ├── sidebar.html
│   │   ├── file-card.html
│   │   ├── viewer.html
│   │   └── tag-editor.html
│   └── static/
│       └── app.js                 ← Alpine.js component definitions
├── Dockerfile                     ← existing CLI archiver image
├── Dockerfile.web                 ← new web UI image
├── docker-compose.example.yml     ← updated with web service
├── go.mod
└── go.sum
```

---

## 10. Out of Scope (v1)

- User authentication / multi-user support (use reverse proxy)
- Editing or re-archiving files via the UI (CLI archiver owns that workflow)
- Mobile-optimised layout (desktop browser is the primary target)
- Real-time push updates when the CLI archiver runs (polling refresh is sufficient)
- Batch download / zip export
- OCR or AI-based auto-tagging
