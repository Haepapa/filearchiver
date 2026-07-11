package proxy

import (
	"context"
	"database/sql"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"filearchiver/internal/db"
)

// WorkerStatus is the current operational state of the worker.
type WorkerStatus string

const (
	StatusRunning WorkerStatus = "running"
	StatusPaused  WorkerStatus = "paused"
	StatusStopped WorkerStatus = "stopped"
)

// Worker is the background proxy generation service.
type Worker struct {
	database *sql.DB
	proxyDir string
	tools    ToolAvailability

	mu          sync.RWMutex
	status      WorkerStatus
	currentFile string // archive_path of the file being converted right now
	cfg         workerCfg

	pauseCh  chan struct{}
	resumeCh chan struct{}
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type workerCfg struct {
	enabled       bool
	paused        bool
	minBytes      int64
	maxWorkers    int
	imageMaxWidth int
	imageQuality  int
	videoMaxWidth int
	videoCRF      int
	useGPU        bool
}

// NewWorker creates a Worker but does not start it.
func NewWorker(database *sql.DB, proxyDir string) *Worker {
	return &Worker{
		database: database,
		proxyDir: proxyDir,
		tools:    DetectTools(),
		status:   StatusStopped,
		pauseCh:  make(chan struct{}, 1),
		resumeCh: make(chan struct{}, 1),
	}
}

// Start initialises settings from the DB, enqueues eligible files, and begins
// the background worker goroutine. Safe to call multiple times (no-op if running).
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.status == StatusRunning || w.status == StatusPaused {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	if err := w.reloadConfig(); err != nil {
		log.Printf("[proxy] failed to load settings: %v", err)
		return
	}

	w.mu.RLock()
	enabled := w.cfg.enabled
	w.mu.RUnlock()
	if !enabled {
		log.Printf("[proxy] disabled — not starting worker")
		return
	}

	// Reset stale 'processing' rows from an unclean shutdown.
	if err := db.ResetProcessingProxies(w.database); err != nil {
		log.Printf("[proxy] reset processing rows: %v", err)
	}

	// Enqueue files that haven't been evaluated yet.
	w.enqueue()

	childCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	w.mu.Lock()
	w.status = StatusRunning
	w.mu.Unlock()

	w.wg.Add(1)
	go w.loop(childCtx)
	log.Printf("[proxy] worker started (proxyDir=%s, tools=%+v)", w.proxyDir, w.tools)
}

// Stop gracefully shuts down the worker.
func (w *Worker) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.status = StatusStopped
	w.mu.Unlock()
	w.wg.Wait()
}

// Pause suspends conversion without stopping the worker goroutine.
func (w *Worker) Pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == StatusRunning {
		w.status = StatusPaused
		select {
		case w.pauseCh <- struct{}{}:
		default:
		}
		if err := db.SetProxySetting(w.database, "paused", "true"); err != nil {
			log.Printf("[proxy] persist pause: %v", err)
		}
	}
}

// Resume unpauses the worker.
func (w *Worker) Resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == StatusPaused {
		w.status = StatusRunning
		select {
		case w.resumeCh <- struct{}{}:
		default:
		}
		if err := db.SetProxySetting(w.database, "paused", "false"); err != nil {
			log.Printf("[proxy] persist resume: %v", err)
		}
	}
}

// Restart resets all failed proxies to pending and resumes the worker.
func (w *Worker) Restart() {
	n, err := db.ResetFailedProxies(w.database)
	if err != nil {
		log.Printf("[proxy] reset failed proxies: %v", err)
	} else {
		log.Printf("[proxy] reset %d failed proxies to pending", n)
	}
	w.enqueue()
	w.Resume()
}

// Reconfigure reloads settings from the DB and re-enqueues files if needed.
// Called after the user saves proxy settings via the API.
func (w *Worker) Reconfigure() {
	if err := w.reloadConfig(); err != nil {
		log.Printf("[proxy] reconfigure: %v", err)
		return
	}
	w.enqueue()

	w.mu.RLock()
	enabled := w.cfg.enabled
	paused := w.cfg.paused
	w.mu.RUnlock()

	if !enabled {
		w.Pause()
		return
	}
	if paused {
		w.Pause()
	} else {
		w.Resume()
	}
}

// Status returns the current worker state, the file currently being converted,
// tool availability, and queue statistics.
func (w *Worker) Status() StatusResponse {
	w.mu.RLock()
	status := w.status
	current := w.currentFile
	tools := w.tools
	w.mu.RUnlock()

	stats, _ := db.GetProxyStats(w.database)

	return StatusResponse{
		Running:     status == StatusRunning,
		Paused:      status == StatusPaused,
		Status:      string(status),
		CurrentFile: current,
		Stats:       stats,
		Tools:       tools,
	}
}

// StatusResponse is the JSON payload returned by GET /api/proxy/status.
type StatusResponse struct {
	Running     bool                 `json:"running"`
	Paused      bool                 `json:"paused"`
	Status      string               `json:"status"`
	CurrentFile string               `json:"current_file,omitempty"`
	Stats       db.ProxyStats        `json:"stats"`
	Tools       ToolAvailability     `json:"tools"`
}

// loop is the main worker goroutine.
func (w *Worker) loop(ctx context.Context) {
	defer w.wg.Done()

	sem := make(chan struct{}, w.maxWorkers())

	for {
		// Check for pause signal.
		select {
		case <-w.pauseCh:
			log.Printf("[proxy] paused")
			select {
			case <-w.resumeCh:
				log.Printf("[proxy] resumed")
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		default:
		}

		items, err := db.ListFilesNeedingProxy(w.database, w.maxWorkers())
		if err != nil {
			log.Printf("[proxy] queue query: %v", err)
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		if len(items) == 0 {
			// Queue empty — wait before polling again.
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		for _, item := range items {
			item := item // capture for goroutine
			sem <- struct{}{}
			w.wg.Add(1)
			go func() {
				defer w.wg.Done()
				defer func() { <-sem }()
				w.process(item)
			}()
		}

		// Small yield between batches to avoid hammering the DB.
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}

// process converts a single file to a proxy.
func (w *Worker) process(item db.ProxyQueueItem) {
	ext := strings.ToLower(item.Extension)

	w.mu.Lock()
	w.currentFile = item.ArchivePath
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.currentFile = ""
		w.mu.Unlock()
	}()

	if err := db.MarkProxyProcessing(w.database, item.ID); err != nil {
		log.Printf("[proxy] mark processing %d: %v", item.ID, err)
		return
	}

	w.mu.RLock()
	cfg := w.cfg
	tools := w.tools
	w.mu.RUnlock()

	var proxyPath string
	var convErr error

	switch {
	case SupportedImageExts[ext]:
		if !tools.ImageMagick {
			_ = db.MarkProxyFailed(w.database, item.ID, "imagemagick not available")
			return
		}
		if IsRaw(ext) && !tools.Dcraw {
			_ = db.MarkProxyFailed(w.database, item.ID, "dcraw not available for RAW conversion")
			return
		}
		proxyPath = ProxyImagePath(item.ArchivePath, w.proxyDir)
		convErr = ConvertImage(item.ArchivePath, proxyPath, ImageConfig{
			MaxWidth: cfg.imageMaxWidth,
			Quality:  cfg.imageQuality,
		})

	case SupportedVideoExts[ext]:
		if !tools.FFmpeg {
			_ = db.MarkProxyFailed(w.database, item.ID, "ffmpeg not available")
			return
		}
		proxyPath = ProxyVideoPath(item.ArchivePath, w.proxyDir)
		convErr = ConvertVideo(item.ArchivePath, proxyPath, VideoConfig{
			MaxWidth: cfg.videoMaxWidth,
			CRF:      cfg.videoCRF,
			UseGPU:   cfg.useGPU && tools.NvidiaGPU,
		})

	default:
		_ = db.MarkProxySkipped(w.database, item.ID)
		return
	}

	if convErr != nil {
		log.Printf("[proxy] convert %s: %v", filepath.Base(item.ArchivePath), convErr)
		_ = db.MarkProxyFailed(w.database, item.ID, convErr.Error())
		return
	}

	if err := db.MarkProxyDone(w.database, item.ID, proxyPath); err != nil {
		log.Printf("[proxy] mark done %d: %v", item.ID, err)
	}
	log.Printf("[proxy] done: %s → %s", filepath.Base(item.ArchivePath), filepath.Base(proxyPath))
}

// enqueue evaluates un-processed files against the current size/ext criteria.
func (w *Worker) enqueue() {
	w.mu.RLock()
	minBytes := w.cfg.minBytes
	w.mu.RUnlock()

	n, err := db.EnqueueEligibleFiles(w.database, minBytes, SupportedExts())
	if err != nil {
		log.Printf("[proxy] enqueue: %v", err)
	} else if n > 0 {
		log.Printf("[proxy] enqueued %d new files for proxy generation", n)
	}
}

// reloadConfig reads proxy_settings from the DB into w.cfg.
func (w *Worker) reloadConfig() error {
	settings, err := db.GetAllProxySettings(w.database)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.cfg = workerCfg{
		enabled:       parseBool(settings["enabled"], true),
		paused:        parseBool(settings["paused"], false),
		minBytes:      parseInt64(settings["min_file_size_mb"], 10) * 1024 * 1024,
		maxWorkers:    parseInt(settings["max_workers"], 1),
		imageMaxWidth: parseInt(settings["image_max_width"], 2048),
		imageQuality:  parseInt(settings["image_quality"], 85),
		videoMaxWidth: parseInt(settings["video_max_width"], 1280),
		videoCRF:      parseInt(settings["video_crf"], 28),
		useGPU:        parseBool(settings["use_gpu"], false),
	}
	return nil
}

func (w *Worker) maxWorkers() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.cfg.maxWorkers < 1 {
		return 1
	}
	return w.cfg.maxWorkers
}

func parseBool(s string, def bool) bool {
	if s == "" {
		return def
	}
	return s == "true" || s == "1"
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return def
	}
	return v
}

func parseInt64(s string, def int64) int64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return def
	}
	return v
}
