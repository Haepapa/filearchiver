package proxy

import (
	"os/exec"
	"strings"
)

// ToolAvailability reports which external conversion tools are present.
type ToolAvailability struct {
	FFmpeg       bool `json:"ffmpeg"`
	ImageMagick  bool `json:"imagemagick"`
	Dcraw        bool `json:"dcraw"`
	NvidiaGPU    bool `json:"gpu"`
}

// DetectTools checks for required external binaries.
func DetectTools() ToolAvailability {
	return ToolAvailability{
		FFmpeg:      hasBin("ffmpeg"),
		ImageMagick: hasBin("convert"),
		Dcraw:       hasBin("dcraw"),
		NvidiaGPU:   hasNvidiaGPU(),
	}
}

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func hasNvidiaGPU() bool {
	out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// SupportedImageExts is the set of lowercase extensions handled by image conversion.
var SupportedImageExts = map[string]bool{
	// RAW camera formats (require dcraw)
	"cr2": true, "cr3": true, "dng": true, "arw": true, "nef": true,
	"orf": true, "rw2": true, "raf": true, "pef": true, "srw": true,
	// Standard images (ImageMagick)
	"tif": true, "tiff": true, "heic": true, "heif": true,
	"bmp": true, "png": true, "webp": true,
	// Large JPEGs are also eligible
	"jpg": true, "jpeg": true,
}

// rawExts identifies camera RAW formats that require dcraw.
var rawExts = map[string]bool{
	"cr2": true, "cr3": true, "dng": true, "arw": true, "nef": true,
	"orf": true, "rw2": true, "raf": true, "pef": true, "srw": true,
}

// SupportedVideoExts is the set of lowercase extensions handled by video conversion.
var SupportedVideoExts = map[string]bool{
	"mov": true, "avi": true, "mkv": true, "m4v": true, "wmv": true,
	"flv": true, "mts": true, "m2ts": true, "mxf": true,
	// Large MP4 originals (e.g. DNXHR-wrapped) are also eligible
	"mp4": true,
}

// SupportedExts is the union of image and video extensions.
func SupportedExts() map[string]bool {
	m := make(map[string]bool, len(SupportedImageExts)+len(SupportedVideoExts))
	for k := range SupportedImageExts {
		m[k] = true
	}
	for k := range SupportedVideoExts {
		m[k] = true
	}
	return m
}

// IsRaw reports whether the extension is a camera RAW format.
func IsRaw(ext string) bool {
	return rawExts[strings.ToLower(ext)]
}
