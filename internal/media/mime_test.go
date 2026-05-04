package media_test

import (
	"testing"

	"filearchiver/internal/media"
)

func TestMIMEForExt(t *testing.T) {
	cases := []struct {
		ext  string
		want string
	}{
		{"jpg", "image/jpeg"},
		{"jpeg", "image/jpeg"},
		{"png", "image/png"},
		{"gif", "image/gif"},
		{"bmp", "image/bmp"},
		{"webp", "image/webp"},
		{"svg", "image/svg+xml"},
		{"mp4", "video/mp4"},
		{"mov", "video/quicktime"},
		{"mp3", "audio/mpeg"},
		{"flac", "audio/flac"},
		{"pdf", "application/pdf"},
		{"txt", "text/plain; charset=utf-8"},
		{"json", "application/json; charset=utf-8"},
		{"xyz", "application/octet-stream"},
		{"", "application/octet-stream"},
	}
	for _, c := range cases {
		got := media.MIMEForExt(c.ext)
		if got != c.want {
			t.Errorf("MIMEForExt(%q) = %q, want %q", c.ext, got, c.want)
		}
	}
}

func TestViewerForExt(t *testing.T) {
	cases := []struct {
		ext  string
		want media.ViewerType
	}{
		{"jpg", media.ViewerImage},
		{"png", media.ViewerImage},
		{"gif", media.ViewerImage},
		{"mp4", media.ViewerVideo},
		{"mov", media.ViewerVideo},
		{"mp3", media.ViewerAudio},
		{"flac", media.ViewerAudio},
		{"pdf", media.ViewerPDF},
		{"txt", media.ViewerText},
		{"json", media.ViewerText},
		{"go", media.ViewerText},
		{"xyz", media.ViewerOther},
	}
	for _, c := range cases {
		got := media.ViewerForExt(c.ext)
		if got != c.want {
			t.Errorf("ViewerForExt(%q) = %v, want %v", c.ext, got, c.want)
		}
	}
}

func TestIsThumbnailable(t *testing.T) {
	yes := []string{"jpg", "jpeg", "png", "gif", "bmp"}
	for _, ext := range yes {
		if !media.IsThumbnailable(ext) {
			t.Errorf("IsThumbnailable(%q) should be true", ext)
		}
	}
	no := []string{"mp4", "mp3", "pdf", "txt", "webp", "xyz"}
	for _, ext := range no {
		if media.IsThumbnailable(ext) {
			t.Errorf("IsThumbnailable(%q) should be false", ext)
		}
	}
}
