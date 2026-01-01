package fswatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/status"
)

// Op describes a normalized filesystem operation.
type Op int

const (
	OpUnknown Op = iota
	OpCreate
	OpWrite
	OpRemove
	OpRename
	OpChmod
)

// Event is a normalized file event.
type Event struct {
	Path string
	Op   Op
	When time.Time
}

// Watcher observes local filesystem changes.
type Watcher struct {
	logger *zap.Logger
	cfg    *config.Config
	status *status.Store

	watcher *fsnotify.Watcher
	out     chan Event

	mu      sync.Mutex
	pending map[string]Event

	debounce time.Duration
}

// NewWatcher constructs a filesystem watcher.
func NewWatcher(logger *zap.Logger, cfg *config.Config, statusStore *status.Store) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		logger:   logger,
		cfg:      cfg,
		status:   statusStore,
		watcher:  w,
		out:      make(chan Event, 256),
		pending:  make(map[string]Event),
		debounce: 300 * time.Millisecond,
	}, nil
}

// Events returns the channel of normalized events.
func (w *Watcher) Events() <-chan Event {
	return w.out
}

// Start begins watching and processing events.
func (w *Watcher) Start(ctx context.Context) error {
	if w.cfg.SyncRoot == "" {
		return nil
	}

	if err := os.MkdirAll(w.cfg.SyncRoot, 0o700); err != nil {
		return err
	}
	if err := w.addRecursive(w.cfg.SyncRoot); err != nil {
		return err
	}

	w.status.Update(status.Snapshot{State: status.StateIdle, Message: "watching"})

	go w.run(ctx)
	return nil
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	if w.watcher != nil {
		return w.watcher.Close()
	}
	return nil
}

func (w *Watcher) run(ctx context.Context) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(evt)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fswatch error", zap.Error(err))
			w.status.Update(status.Snapshot{State: status.StateError, Message: "fswatch error"})
		case <-ticker.C:
			w.flushPending()
		}
	}
}

func (w *Watcher) handleEvent(evt fsnotify.Event) {
	path := evt.Name
	if w.shouldIgnore(path) {
		return
	}

	if evt.Op&fsnotify.Create == fsnotify.Create {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			_ = w.addRecursive(path)
		}
	}

	op := normalizeOp(evt.Op)
	if op == OpUnknown {
		return
	}

	w.mu.Lock()
	w.pending[path] = Event{Path: path, Op: op, When: time.Now().Add(w.debounce)}
	w.mu.Unlock()
}

func (w *Watcher) flushPending() {
	now := time.Now()
	var ready []Event

	w.mu.Lock()
	for path, evt := range w.pending {
		if evt.When.Before(now) || evt.When.Equal(now) {
			ready = append(ready, Event{Path: path, Op: evt.Op, When: now})
			delete(w.pending, path)
		}
	}
	w.mu.Unlock()

	for _, evt := range ready {
		w.status.SetLastEvent(formatEvent(evt, w.cfg.SyncRoot))
		select {
		case w.out <- evt:
		default:
			w.logger.Warn("fswatch event dropped", zap.String("path", evt.Path))
		}
	}
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if w.shouldIgnore(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if err := w.watcher.Add(path); err != nil {
				return err
			}
		}
		return nil
	})
}

func (w *Watcher) shouldIgnore(path string) bool {
	base := filepath.Base(path)
	if base == "." || base == ".." {
		return true
	}

	if w.cfg.LogFilePath != "" && path == w.cfg.LogFilePath {
		return true
	}
	if w.cfg.DatabasePath != "" && path == w.cfg.DatabasePath {
		return true
	}
	if w.cfg.SocketPath != "" && path == w.cfg.SocketPath {
		return true
	}

	for _, pat := range w.cfg.IgnorePatterns {
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
	}

	suffixes := []string{".swp", ".tmp", "~", ".DS_Store"}
	for _, suf := range suffixes {
		if len(base) >= len(suf) && base[len(base)-len(suf):] == suf {
			return true
		}
	}

	return false
}

func normalizeOp(op fsnotify.Op) Op {
	switch {
	case op&fsnotify.Create == fsnotify.Create:
		return OpCreate
	case op&fsnotify.Write == fsnotify.Write:
		return OpWrite
	case op&fsnotify.Remove == fsnotify.Remove:
		return OpRemove
	case op&fsnotify.Rename == fsnotify.Rename:
		return OpRename
	case op&fsnotify.Chmod == fsnotify.Chmod:
		return OpChmod
	default:
		return OpUnknown
	}
}

func formatEvent(evt Event, root string) string {
	path := evt.Path
	if root != "" {
		if rel, err := filepath.Rel(root, evt.Path); err == nil {
			path = rel
		}
	}
	return fmt.Sprintf("%s %s", opString(evt.Op), path)
}

func opString(op Op) string {
	switch op {
	case OpCreate:
		return "CREATE"
	case OpWrite:
		return "WRITE"
	case OpRemove:
		return "REMOVE"
	case OpRename:
		return "RENAME"
	case OpChmod:
		return "CHMOD"
	default:
		return "UNKNOWN"
	}
}
