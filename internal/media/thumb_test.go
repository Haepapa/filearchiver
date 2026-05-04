package media_test

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"filearchiver/internal/media"
)

// createTestPNG writes a solid-colour PNG to disk and returns its path.
func createTestPNG(t *testing.T, w, h int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 149, B: 237, A: 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// createTestJPEG writes a JPEG to disk.
func createTestJPEG(t *testing.T, w, h int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jpg")

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestThumbnailPath(t *testing.T) {
	p1 := media.ThumbnailPath("/archive/2024/01/photo.jpg", "/thumbs")
	p2 := media.ThumbnailPath("/archive/2024/01/photo.jpg", "/thumbs")
	p3 := media.ThumbnailPath("/archive/2024/02/other.jpg", "/thumbs")

	if p1 != p2 {
		t.Errorf("same input should produce same path: %q vs %q", p1, p2)
	}
	if p1 == p3 {
		t.Errorf("different inputs should produce different paths: %q", p1)
	}
	if filepath.Ext(p1) != ".jpg" {
		t.Errorf("thumbnail path should have .jpg extension, got %q", p1)
	}
}

func TestGenerateThumbnail_PNG(t *testing.T) {
	src := createTestPNG(t, 500, 400)
	thumbDir := t.TempDir()

	thumbPath, err := media.GenerateThumbnail(src, thumbDir)
	if err != nil {
		t.Fatalf("GenerateThumbnail error: %v", err)
	}

	info, err := os.Stat(thumbPath)
	if err != nil {
		t.Fatalf("thumbnail file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("thumbnail file is empty")
	}

	// Verify it's a valid JPEG.
	f, err := os.Open(thumbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("thumbnail is not a valid JPEG: %v", err)
	}

	// Check dimensions are within bounds.
	b := img.Bounds()
	if b.Dx() > media.ThumbSize || b.Dy() > media.ThumbSize {
		t.Errorf("thumbnail too large: %dx%d (max %d)", b.Dx(), b.Dy(), media.ThumbSize)
	}
}

func TestGenerateThumbnail_JPEG(t *testing.T) {
	src := createTestJPEG(t, 1920, 1080)
	thumbDir := t.TempDir()

	thumbPath, err := media.GenerateThumbnail(src, thumbDir)
	if err != nil {
		t.Fatalf("GenerateThumbnail error: %v", err)
	}

	f, err := os.Open(thumbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("thumbnail is not a valid JPEG: %v", err)
	}

	b := img.Bounds()
	if b.Dx() > media.ThumbSize || b.Dy() > media.ThumbSize {
		t.Errorf("thumbnail dimensions exceed ThumbSize: %dx%d", b.Dx(), b.Dy())
	}
}

func TestGenerateThumbnail_SmallImage(t *testing.T) {
	// Image smaller than ThumbSize should be accepted as-is.
	src := createTestPNG(t, 64, 48)
	thumbDir := t.TempDir()

	thumbPath, err := media.GenerateThumbnail(src, thumbDir)
	if err != nil {
		t.Fatalf("GenerateThumbnail error: %v", err)
	}

	f, err := os.Open(thumbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("thumbnail is not a valid JPEG: %v", err)
	}

	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 48 {
		t.Errorf("small image should not be upscaled, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestGenerateThumbnail_Cached(t *testing.T) {
	src := createTestPNG(t, 300, 200)
	thumbDir := t.TempDir()

	path1, err := media.GenerateThumbnail(src, thumbDir)
	if err != nil {
		t.Fatal(err)
	}

	// Modify the source so we can detect if it's re-read.
	os.Remove(src)

	// Should return cached thumbnail path without error.
	path2, err := media.GenerateThumbnail(src, thumbDir)
	if err != nil {
		t.Fatalf("cached thumbnail should not error: %v", err)
	}
	if path1 != path2 {
		t.Errorf("should return same cached path: %q vs %q", path1, path2)
	}
}

func TestGenerateThumbnail_NotAnImage(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(src, []byte("not an image"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := media.GenerateThumbnail(src, t.TempDir())
	if err == nil {
		t.Error("should return error for non-image file")
	}
}

func TestGenerateThumbnail_MissingFile(t *testing.T) {
	_, err := media.GenerateThumbnail("/nonexistent/path/image.png", t.TempDir())
	if err == nil {
		t.Error("should return error for missing source file")
	}
}
