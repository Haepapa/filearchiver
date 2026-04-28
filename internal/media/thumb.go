package media

import (
	"crypto/md5"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/gif"  // register GIF decoder
	_ "image/png"  // register PNG decoder
	"os"
	"path/filepath"

	_ "golang.org/x/image/bmp"  // register BMP decoder
	"golang.org/x/image/draw"
)

// ThumbSize is the maximum dimension (width or height) for generated thumbnails.
const ThumbSize = 256

// ThumbnailPath returns the deterministic cache path for a given source file.
// The filename is an MD5 hash of the source path, with a .jpg extension.
func ThumbnailPath(srcPath, thumbDir string) string {
	hash := md5.Sum([]byte(srcPath))
	return filepath.Join(thumbDir, fmt.Sprintf("%x.jpg", hash))
}

// GenerateThumbnail returns the path to a cached JPEG thumbnail for srcPath,
// creating it if necessary. The thumbnail is stored in thumbDir.
// Returns an error if the source file cannot be decoded as an image.
func GenerateThumbnail(srcPath, thumbDir string) (string, error) {
	thumbPath := ThumbnailPath(srcPath, thumbDir)

	// Cache hit: return existing thumbnail immediately.
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbPath, nil
	}

	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return "", fmt.Errorf("create thumb dir: %w", err)
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open source image: %w", err)
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}

	scaled := scaleImage(src, ThumbSize)

	out, err := os.Create(thumbPath)
	if err != nil {
		return "", fmt.Errorf("create thumbnail file: %w", err)
	}
	defer out.Close()

	if err := jpeg.Encode(out, scaled, &jpeg.Options{Quality: 82}); err != nil {
		// Clean up partial file on encode failure.
		os.Remove(thumbPath)
		return "", fmt.Errorf("encode thumbnail: %w", err)
	}

	return thumbPath, nil
}

// scaleImage returns a new RGBA image that fits within maxSize×maxSize,
// preserving the original aspect ratio. Uses bilinear interpolation.
func scaleImage(src image.Image, maxSize int) *image.RGBA {
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Already small enough — just convert format without scaling.
	if w <= maxSize && h <= maxSize {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		return dst
	}

	var dw, dh int
	if w >= h {
		dw = maxSize
		dh = int(float64(h) * float64(maxSize) / float64(w))
	} else {
		dh = maxSize
		dw = int(float64(w) * float64(maxSize) / float64(h))
	}
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}
