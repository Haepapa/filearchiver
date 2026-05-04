package db

import (
	"database/sql"
	"fmt"
	"time"
)

// TagCategory is a grouping for tags (e.g. "People", "Places", "Projects").
type TagCategory struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

// Tag is a user-defined label that can be attached to any number of files.
type Tag struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	CategoryID    *int64    `json:"category_id"`
	CategoryName  string    `json:"category_name,omitempty"`
	CategoryColor string    `json:"category_color,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	FileCount     int64     `json:"file_count"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Tag categories
// ──────────────────────────────────────────────────────────────────────────────

// ListTagCategories returns all tag categories ordered by name.
func ListTagCategories(database *sql.DB) ([]TagCategory, error) {
	rows, err := database.Query(
		`SELECT id, name, color, created_at FROM tag_categories ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tag categories: %w", err)
	}
	defer rows.Close()

	var cats []TagCategory
	for rows.Next() {
		var c TagCategory
		var tsStr string
		if err := rows.Scan(&c.ID, &c.Name, &c.Color, &tsStr); err != nil {
			return nil, err
		}
		c.CreatedAt = parseTime(tsStr)
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// CreateTagCategory inserts a new category and returns the populated struct.
func CreateTagCategory(database *sql.DB, name, color string) (TagCategory, error) {
	if color == "" {
		color = "#6b7280"
	}
	res, err := database.Exec(
		`INSERT INTO tag_categories (name, color) VALUES (?, ?)`, name, color)
	if err != nil {
		return TagCategory{}, fmt.Errorf("create tag category: %w", err)
	}
	id, _ := res.LastInsertId()
	return TagCategory{ID: id, Name: name, Color: color, CreatedAt: time.Now()}, nil
}

// UpdateTagCategory renames or recolors an existing category.
// Only non-empty fields are updated.
func UpdateTagCategory(database *sql.DB, id int64, name, color string) error {
	if name == "" && color == "" {
		return nil
	}
	if name != "" && color != "" {
		_, err := database.Exec(
			`UPDATE tag_categories SET name=?, color=? WHERE id=?`, name, color, id)
		return err
	}
	if name != "" {
		_, err := database.Exec(`UPDATE tag_categories SET name=? WHERE id=?`, name, id)
		return err
	}
	_, err := database.Exec(`UPDATE tag_categories SET color=? WHERE id=?`, color, id)
	return err
}

// DeleteTagCategory deletes a category. Tags belonging to it become uncategorised
// (category_id → NULL) because of the ON DELETE SET NULL foreign key.
func DeleteTagCategory(database *sql.DB, id int64) error {
	_, err := database.Exec(`DELETE FROM tag_categories WHERE id=?`, id)
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// Tags
// ──────────────────────────────────────────────────────────────────────────────

// ListTags returns all tags joined with their category. If categoryID > 0, only
// tags for that category are returned. Results are ordered by category name then
// tag name.
func ListTags(database *sql.DB, categoryID int64) ([]Tag, error) {
	query := `
		SELECT
			t.id, t.name, t.category_id,
			COALESCE(tc.name,  '') AS cat_name,
			COALESCE(tc.color, '') AS cat_color,
			t.created_at,
			COUNT(ft.file_id) AS file_count
		FROM tags t
		LEFT JOIN tag_categories tc ON tc.id = t.category_id
		LEFT JOIN file_tags ft      ON ft.tag_id = t.id`
	var args []any
	if categoryID > 0 {
		query += ` WHERE t.category_id = ?`
		args = append(args, categoryID)
	}
	query += ` GROUP BY t.id ORDER BY cat_name, t.name`

	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	return scanTags(rows)
}

// GetTag returns a single tag by ID, or nil if not found.
func GetTag(database *sql.DB, id int64) (*Tag, error) {
	rows, err := database.Query(`
		SELECT
			t.id, t.name, t.category_id,
			COALESCE(tc.name,  '') AS cat_name,
			COALESCE(tc.color, '') AS cat_color,
			t.created_at,
			COUNT(ft.file_id) AS file_count
		FROM tags t
		LEFT JOIN tag_categories tc ON tc.id = t.category_id
		LEFT JOIN file_tags ft      ON ft.tag_id = t.id
		WHERE t.id = ?
		GROUP BY t.id
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags, err := scanTags(rows)
	if err != nil || len(tags) == 0 {
		return nil, err
	}
	return &tags[0], nil
}

// CreateTag creates a new tag with the given name and optional category.
// Pass categoryID=0 to create an uncategorised tag.
func CreateTag(database *sql.DB, name string, categoryID int64) (Tag, error) {
	var res sql.Result
	var err error
	if categoryID > 0 {
		res, err = database.Exec(
			`INSERT INTO tags (name, category_id) VALUES (?, ?)`, name, categoryID)
	} else {
		res, err = database.Exec(
			`INSERT INTO tags (name, category_id) VALUES (?, NULL)`, name)
	}
	if err != nil {
		return Tag{}, fmt.Errorf("create tag: %w", err)
	}
	id, _ := res.LastInsertId()
	t, err := GetTag(database, id)
	if err != nil || t == nil {
		return Tag{ID: id, Name: name}, err
	}
	return *t, nil
}

// UpdateTag changes a tag's name and/or category. Pass categoryID=-1 to leave
// the category unchanged; pass 0 to set it to NULL (uncategorised).
func UpdateTag(database *sql.DB, id int64, name string, categoryID int64, changeCat bool) error {
	switch {
	case name != "" && changeCat && categoryID > 0:
		_, err := database.Exec(
			`UPDATE tags SET name=?, category_id=? WHERE id=?`, name, categoryID, id)
		return err
	case name != "" && changeCat && categoryID == 0:
		_, err := database.Exec(
			`UPDATE tags SET name=?, category_id=NULL WHERE id=?`, name, id)
		return err
	case name != "" && !changeCat:
		_, err := database.Exec(`UPDATE tags SET name=? WHERE id=?`, name, id)
		return err
	case name == "" && changeCat && categoryID > 0:
		_, err := database.Exec(
			`UPDATE tags SET category_id=? WHERE id=?`, categoryID, id)
		return err
	case name == "" && changeCat && categoryID == 0:
		_, err := database.Exec(`UPDATE tags SET category_id=NULL WHERE id=?`, id)
		return err
	default:
		return nil
	}
}

// DeleteTag deletes a tag by ID. The file_tags rows are removed automatically
// via the ON DELETE CASCADE foreign key.
func DeleteTag(database *sql.DB, id int64) error {
	_, err := database.Exec(`DELETE FROM tags WHERE id=?`, id)
	return err
}

// MergeTags reassigns all file_tags from sourceID to intoID, then deletes the
// source tag. Any file already tagged with intoID is handled gracefully via the
// INSERT OR IGNORE.
func MergeTags(database *sql.DB, sourceID, intoID int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Re-point every file that has the source tag → the target tag.
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO file_tags (file_id, tag_id)
		SELECT file_id, ? FROM file_tags WHERE tag_id = ?
	`, intoID, sourceID); err != nil {
		return fmt.Errorf("merge reassign: %w", err)
	}
	// Remove old join rows (the source tag).
	if _, err := tx.Exec(
		`DELETE FROM file_tags WHERE tag_id = ?`, sourceID); err != nil {
		return fmt.Errorf("merge delete old rows: %w", err)
	}
	// Delete the source tag itself.
	if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, sourceID); err != nil {
		return fmt.Errorf("merge delete tag: %w", err)
	}
	return tx.Commit()
}

// ──────────────────────────────────────────────────────────────────────────────
// File-tag associations
// ──────────────────────────────────────────────────────────────────────────────

// GetFileTags returns all tags attached to a file.
func GetFileTags(database *sql.DB, fileID int64) ([]Tag, error) {
	rows, err := database.Query(`
		SELECT
			t.id, t.name, t.category_id,
			COALESCE(tc.name,  '') AS cat_name,
			COALESCE(tc.color, '') AS cat_color,
			t.created_at,
			0 AS file_count
		FROM file_tags ft
		JOIN tags t             ON t.id  = ft.tag_id
		LEFT JOIN tag_categories tc ON tc.id = t.category_id
		WHERE ft.file_id = ?
		ORDER BY cat_name, t.name
	`, fileID)
	if err != nil {
		return nil, fmt.Errorf("get file tags: %w", err)
	}
	defer rows.Close()
	tags, err := scanTags(rows)
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []Tag{}
	}
	return tags, nil
}

// SetFileTags replaces the complete set of tags for a file in a single
// transaction. tagIDs may be empty to remove all tags.
func SetFileTags(database *sql.DB, fileID int64, tagIDs []int64) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM file_tags WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	for _, tid := range tagIDs {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO file_tags (file_id, tag_id) VALUES (?, ?)`,
			fileID, tid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ──────────────────────────────────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────────────────────────────────

func scanTags(rows *sql.Rows) ([]Tag, error) {
	var tags []Tag
	for rows.Next() {
		var t Tag
		var catID sql.NullInt64
		var tsStr string
		if err := rows.Scan(
			&t.ID, &t.Name, &catID,
			&t.CategoryName, &t.CategoryColor,
			&tsStr, &t.FileCount,
		); err != nil {
			return nil, err
		}
		if catID.Valid {
			v := catID.Int64
			t.CategoryID = &v
		}
		t.CreatedAt = parseTime(tsStr)
		tags = append(tags, t)
	}
	return tags, rows.Err()
}
