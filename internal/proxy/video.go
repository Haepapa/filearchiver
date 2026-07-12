package proxy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// VideoConfig holds quality/resize settings for video proxy generation.
type VideoConfig struct {
	MaxWidth int  // pixels for the longer edge; 0 = 1280
	CRF      int  // H.264 CRF value (lower = better quality); 0 = 28
	UseGPU   bool // use NVIDIA NVENC when true
}

// ConvertVideo generates an H.264 MP4 proxy for the given source video.
// Returns the path to the generated proxy file.
func ConvertVideo(srcPath, proxyPath string, cfg VideoConfig) error {
	if err := os.MkdirAll(filepath.Dir(proxyPath), 0755); err != nil {
		return fmt.Errorf("create proxy dir: %w", err)
	}

	maxW := cfg.MaxWidth
	if maxW <= 0 {
		maxW = 1280
	}
	crf := cfg.CRF
	if crf <= 0 {
		crf = 28
	}

	// Scale filter: limit width to maxW while preserving aspect ratio;
	// height must be divisible by 2 (H.264 requirement).
	scaleFilter := fmt.Sprintf("scale='min(%d,iw)':'-2'", maxW)

	var args []string
	if cfg.UseGPU {
		args = buildGPUArgs(srcPath, proxyPath, scaleFilter, crf)
	} else {
		args = buildCPUArgs(srcPath, proxyPath, scaleFilter, crf)
	}

	cmd := exec.Command("nice", append([]string{"-n", "19", "ffmpeg"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w — %s", err, truncate(string(out), 400))
	}
	return nil
}

func buildCPUArgs(src, dst, scaleFilter string, crf int) []string {
	return []string{
		"-i", src,
		"-vf", scaleFilter,
		"-c:v", "libx264",
		"-crf", fmt.Sprintf("%d", crf),
		"-preset", "fast",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y", dst,
	}
}

func buildGPUArgs(src, dst, scaleFilter string, crf int) []string {
	return []string{
		"-i", src,
		"-vf", scaleFilter,
		"-c:v", "h264_nvenc",
		"-cq", fmt.Sprintf("%d", crf),
		"-preset", "fast",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y", dst,
	}
}

// ProxyVideoPath returns the deterministic proxy path for a video source file.
// Proxy is always an .mp4 regardless of source extension.
func ProxyVideoPath(srcPath, proxyDir string) string {
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	rel, _ := filepath.Rel("/", srcPath)
	dir := filepath.Join(proxyDir, filepath.Dir(rel))
	return filepath.Join(dir, base+"_proxy.mp4")
}

// ProxyImagePath returns the deterministic proxy path for an image source file.
// Proxy is always a .jpg regardless of source extension.
func ProxyImagePath(srcPath, proxyDir string) string {
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	rel, _ := filepath.Rel("/", srcPath)
	dir := filepath.Join(proxyDir, filepath.Dir(rel))
	return filepath.Join(dir, base+"_proxy.jpg")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
