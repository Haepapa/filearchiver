package media

import "strings"

// ViewerType describes how the web UI should render a file.
type ViewerType string

const (
	ViewerImage ViewerType = "image"
	ViewerVideo ViewerType = "video"
	ViewerAudio ViewerType = "audio"
	ViewerPDF   ViewerType = "pdf"
	ViewerText  ViewerType = "text"
	ViewerOther ViewerType = "other"
)

var mimeTypes = map[string]string{
	// Images
	"jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png",
	"gif": "image/gif", "webp": "image/webp", "svg": "image/svg+xml",
	"bmp": "image/bmp", "tiff": "image/tiff", "tif": "image/tiff",
	"ico": "image/x-icon", "heic": "image/heic", "heif": "image/heif",
	// Video
	"mp4": "video/mp4", "webm": "video/webm", "mov": "video/quicktime",
	"avi": "video/x-msvideo", "mkv": "video/x-matroska", "m4v": "video/x-m4v",
	"wmv": "video/x-ms-wmv", "flv": "video/x-flv",
	// Audio
	"mp3": "audio/mpeg", "wav": "audio/wav", "flac": "audio/flac",
	"aac": "audio/aac", "m4a": "audio/mp4", "ogg": "audio/ogg",
	"opus": "audio/ogg", "wma": "audio/x-ms-wma",
	// Documents
	"pdf": "application/pdf",
	// Text / code
	"txt":  "text/plain; charset=utf-8",
	"md":   "text/plain; charset=utf-8",
	"json": "application/json; charset=utf-8",
	"yaml": "text/plain; charset=utf-8",
	"yml":  "text/plain; charset=utf-8",
	"csv":  "text/csv; charset=utf-8",
	"xml":  "text/xml; charset=utf-8",
	"log":  "text/plain; charset=utf-8",
	"html": "text/html; charset=utf-8",
	"htm":  "text/html; charset=utf-8",
	"css":  "text/css; charset=utf-8",
	"js":   "text/javascript; charset=utf-8",
	"ts":   "text/plain; charset=utf-8",
	"go":   "text/plain; charset=utf-8",
	"py":   "text/plain; charset=utf-8",
	"java": "text/plain; charset=utf-8",
	"c":    "text/plain; charset=utf-8",
	"cpp":  "text/plain; charset=utf-8",
	"h":    "text/plain; charset=utf-8",
	"sh":   "text/plain; charset=utf-8",
	"bash": "text/plain; charset=utf-8",
	"sql":  "text/plain; charset=utf-8",
	"toml": "text/plain; charset=utf-8",
	"ini":  "text/plain; charset=utf-8",
	"conf": "text/plain; charset=utf-8",
}

var imageExts = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true,
	"webp": true, "svg": true, "bmp": true, "tiff": true,
	"tif": true, "ico": true, "heic": true, "heif": true,
}
var videoExts = map[string]bool{
	"mp4": true, "webm": true, "mov": true, "avi": true,
	"mkv": true, "m4v": true, "wmv": true, "flv": true,
}
var audioExts = map[string]bool{
	"mp3": true, "wav": true, "flac": true, "aac": true,
	"m4a": true, "ogg": true, "opus": true, "wma": true,
}
var textExts = map[string]bool{
	"txt": true, "md": true, "json": true, "yaml": true, "yml": true,
	"csv": true, "xml": true, "log": true, "js": true, "ts": true,
	"go": true, "py": true, "java": true, "c": true, "cpp": true,
	"h": true, "sh": true, "bash": true, "html": true, "htm": true,
	"css": true, "sql": true, "toml": true, "ini": true, "conf": true,
}

// MIMEForExt returns the MIME type string for a lowercase file extension (no dot).
// Returns "application/octet-stream" for unknown extensions.
func MIMEForExt(ext string) string {
	ext = strings.ToLower(ext)
	if m, ok := mimeTypes[ext]; ok {
		return m
	}
	return "application/octet-stream"
}

// ViewerForExt returns the viewer type appropriate for the given extension.
func ViewerForExt(ext string) ViewerType {
	ext = strings.ToLower(ext)
	switch {
	case imageExts[ext]:
		return ViewerImage
	case videoExts[ext]:
		return ViewerVideo
	case audioExts[ext]:
		return ViewerAudio
	case ext == "pdf":
		return ViewerPDF
	case textExts[ext]:
		return ViewerText
	default:
		return ViewerOther
	}
}

// IsThumbnailable reports whether the extension supports server-side thumbnail
// generation using the standard library image decoders.
func IsThumbnailable(ext string) bool {
	switch strings.ToLower(ext) {
	case "jpg", "jpeg", "png", "gif", "bmp":
		return true
	}
	return false
}
