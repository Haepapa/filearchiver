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
// Output is always browser-compatible: H.264 High profile, yuv420p, AAC audio,
// faststart MP4. These constraints ensure playback in all modern browsers.
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

	// Combine scale + pixel-format conversion in one filter chain.
	// format=yuv420p is mandatory for H.264 browser playback — professional
	// sources (DNXHR, ProRes, etc.) use yuv422p or 10-bit which browsers reject.
	// scale uses trunc to guarantee width/height divisible by 2 (H.264 requirement).
	vf := fmt.Sprintf("scale=trunc(min(%d\\,iw)/2)*2:-2,format=yuv420p", maxW)

	var args []string
	if cfg.UseGPU {
		args = buildGPUArgs(srcPath, proxyPath, vf, crf)
	} else {
		args = buildCPUArgs(srcPath, proxyPath, vf, crf)
	}

	cmd := exec.Command("nice", append([]string{"-n", "19", "ffmpeg"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w — %s", err, truncate(string(out), 400))
	}
	return nil
}

func buildCPUArgs(src, dst, vf string, crf int) []string {
	return []string{
		"-i", src,
		"-vf", vf,
		"-c:v", "libx264",
		"-profile:v", "high",   // H.264 High profile — universally supported
		"-level", "4.0",        // compatible with all modern browsers and devices
		"-crf", fmt.Sprintf("%d", crf),
		"-preset", "fast",
		// Audio: re-encode to stereo AAC; -ac 2 handles mono/multichannel sources.
		// If the source has no audio stream this is silently ignored by ffmpeg.
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		"-movflags", "+faststart", // moov atom first — essential for browser streaming
		"-y", dst,
	}
}

func buildGPUArgs(src, dst, vf string, crf int) []string {
	return []string{
		"-i", src,
		"-vf", vf,
		"-c:v", "h264_nvenc",
		"-profile:v", "high",
		"-level", "4.0",
		"-cq", fmt.Sprintf("%d", crf),
		"-preset", "fast",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
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
