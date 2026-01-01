package daemon

import (
	"context"

	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/auth"
	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/fswatch"
	"github.com/sandeepkv93/googlysync/internal/ipc"
	"github.com/sandeepkv93/googlysync/internal/storage"
	syncer "github.com/sandeepkv93/googlysync/internal/sync"
)

// Daemon wires together core services.
type Daemon struct {
	Logger  *zap.Logger
	Config  *config.Config
	Storage *storage.Storage
	Auth    *auth.Service
	Sync    *syncer.Engine
	Watcher *fswatch.Watcher
	IPC     *ipc.Server
}

// NewDaemon constructs a daemon.
func NewDaemon(
	logger *zap.Logger,
	cfg *config.Config,
	store *storage.Storage,
	authSvc *auth.Service,
	syncEngine *syncer.Engine,
	watcher *fswatch.Watcher,
	ipcServer *ipc.Server,
) (*Daemon, error) {
	logger.Info("daemon initialized")
	return &Daemon{
		Logger:  logger,
		Config:  cfg,
		Storage: store,
		Auth:    authSvc,
		Sync:    syncEngine,
		Watcher: watcher,
		IPC:     ipcServer,
	}, nil
}

// Run starts the daemon loop and blocks until shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.Logger.Info("daemon running")

	syncCtx, syncCancel := context.WithCancel(ctx)
	if d.Sync != nil {
		go d.Sync.Run(syncCtx)
	}

	if d.Watcher != nil {
		if err := d.Watcher.Start(syncCtx); err != nil {
			d.Logger.Warn("fswatch start failed", zap.Error(err))
		}
	}

	errCh := make(chan error, 1)
	go func() {
		if d.IPC == nil {
			errCh <- nil
			return
		}
		errCh <- d.IPC.Start(ctx)
	}()

	select {
	case <-ctx.Done():
		syncCancel()
		if d.IPC != nil {
			d.IPC.Stop()
		}
		d.Logger.Info("daemon shutting down")
		return d.Close()
	case err := <-errCh:
		syncCancel()
		if err != nil {
			return err
		}
		return d.Close()
	}
}

// Close releases resources owned by the daemon.
func (d *Daemon) Close() error {
	if d.Watcher != nil {
		_ = d.Watcher.Close()
	}
	if d.Storage != nil {
		return d.Storage.Close()
	}
	return nil
}
