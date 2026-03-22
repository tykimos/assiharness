package config

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Watcher periodically checks config files for changes and reloads if needed.
type Watcher struct {
	configDir   string
	interval    time.Duration
	current     *Config
	mu          sync.RWMutex
	lastModTime time.Time
	onChange    func(*Config)
}

// NewWatcher creates a new Watcher. initial is the starting config.
func NewWatcher(configDir string, interval time.Duration, initial *Config, onChange func(*Config)) *Watcher {
	return &Watcher{
		configDir: configDir,
		interval:  interval,
		current:   initial,
		onChange:  onChange,
	}
}

// Start runs the watch loop in a goroutine. It stops when ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Printf("config watcher stopped")
				return
			case <-ticker.C:
				w.checkAndReload()
			}
		}
	}()
}

// Current returns the most recently loaded config, safe for concurrent use.
func (w *Watcher) Current() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

// checkAndReload walks the config directory, finds the latest modification
// time, and reloads when it is newer than the last seen mod time.
func (w *Watcher) checkAndReload() {
	latest, err := latestModTime(w.configDir)
	if err != nil {
		log.Printf("config watcher: error scanning %s: %v", w.configDir, err)
		return
	}

	w.mu.RLock()
	unchanged := !latest.After(w.lastModTime)
	w.mu.RUnlock()

	if unchanged {
		return
	}

	cfg, err := LoadConfig(w.configDir)
	if err != nil {
		log.Printf("config watcher: reload failed: %v", err)
		return
	}

	w.mu.Lock()
	w.current = cfg
	w.lastModTime = latest
	w.mu.Unlock()

	log.Printf("config watcher: reloaded config from %s (mod time %s)", w.configDir, latest.Format(time.RFC3339))

	if w.onChange != nil {
		w.onChange(cfg)
	}
}

// latestModTime returns the most recent modification time among all files
// in the given directory tree.
func latestModTime(dir string) (time.Time, error) {
	var latest time.Time

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})

	return latest, err
}
