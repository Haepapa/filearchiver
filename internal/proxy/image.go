package proxy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ImageConfig holds quality/resize settings for image proxy generation.
type ImageConfig struct {
	MaxWidth int // pixels; 0 = no resize
	Quality  int // JPEG quality 1–100
}

// ConvertImage generates a JPEG proxy for the given source image.
// It detects RAW files and uses dcraw; everything else goes through ImageMagick.
// Returns the path to the generated proxy file.
func ConvertImage(srcPath, proxyPath string, cfg ImageConfig) error {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(srcPath), "."))

	if err := os.MkdirAll(filepath.Dir(proxyPath), 0755); err != nil {
		return fmt.Errorf("create proxy dir: %w", err)
	}

	if IsRaw(ext) {
		return convertRaw(srcPath, proxyPath, cfg)
	}
	return convertStandard(srcPath, proxyPath, cfg)
}

// convertRaw uses dcraw to demosaic the RAW file and pipes the TIFF output
// into ImageMagick for resizing and JPEG encoding.
func convertRaw(srcPath, proxyPath string, cfg ImageConfig) error {
	resizeArg := resizeGeometry(cfg.MaxWidth)
	qualityArg := fmt.Sprintf("%d", cfg.Quality)

	// dcraw -c : write to stdout, -w : use camera white balance, -T : TIFF output
	dcrawCmd := exec.Command("nice", "-n", "19",
		"dcraw", "-c", "-w", "-T", srcPath)
	convertCmd := exec.Command("nice", "-n", "19",
		"convert", "-", "-resize", resizeArg, "-quality", qualityArg, "-auto-orient", proxyPath)

	pipe, err := dcrawCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe setup: %w", err)
	}
	convertCmd.Stdin = pipe

	if err := dcrawCmd.Start(); err != nil {
		return fmt.Errorf("dcraw start: %w", err)
	}
	if err := convertCmd.Start(); err != nil {
		_ = dcrawCmd.Process.Kill()
		return fmt.Errorf("convert start: %w", err)
	}

	dcrawErr := dcrawCmd.Wait()
	convertErr := convertCmd.Wait()

	if dcrawErr != nil {
		return fmt.Errorf("dcraw: %w", dcrawErr)
	}
	if convertErr != nil {
		return fmt.Errorf("convert: %w", convertErr)
	}
	return nil
}

// convertStandard uses ImageMagick to convert a standard image to a JPEG proxy.
func convertStandard(srcPath, proxyPath string, cfg ImageConfig) error {
	resizeArg := resizeGeometry(cfg.MaxWidth)
	qualityArg := fmt.Sprintf("%d", cfg.Quality)

	// [0] selects the first frame/layer (handles multi-layer TIFFs, HEICs, etc.)
	cmd := exec.Command("nice", "-n", "19",
		"convert", srcPath+"[0]",
		"-resize", resizeArg,
		"-quality", qualityArg,
		"-auto-orient",
		proxyPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("convert: %w — %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// resizeGeometry returns an ImageMagick geometry string that shrinks the image
// to fit within maxWidth pixels on the longest side, without upscaling.
// e.g. maxWidth=2048 → "2048x2048>"
func resizeGeometry(maxWidth int) string {
	if maxWidth <= 0 {
		return "2048x2048>"
	}
	return fmt.Sprintf("%dx%d>", maxWidth, maxWidth)
}
