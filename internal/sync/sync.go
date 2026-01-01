package sync

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/fswatch"
	"github.com/sandeepkv93/googlysync/internal/status"
	"github.com/sandeepkv93/googlysync/internal/storage"
)

// Engine coordinates sync operations.
type Engine struct {
	Logger *zap.Logger
	Store  *storage.Storage
	Status *status.Store
	Queue  *Queue
}

// NewEngine constructs a sync engine.
func NewEngine(logger *zap.Logger, store *storage.Storage, statusStore *status.Store, queue *Queue) (*Engine, error) {
	logger.Info("sync engine initialized")
	return &Engine{Logger: logger, Store: store, Status: statusStore, Queue: queue}, nil
}

// Run runs a stub sync loop that updates status periodically.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var queueCh <-chan fswatch.Event
	if e.Queue != nil {
		queueCh = e.Queue.Channel()
	}

	for {
		select {
		case <-ctx.Done():
			if e.Status != nil {
				e.Status.Update(status.Snapshot{State: status.StateIdle, Message: "idle"})
			}
			return
		case evt := <-queueCh:
			e.handleEvent(evt)
		case <-ticker.C:
			if e.Status != nil {
				e.Status.Update(status.Snapshot{State: status.StateSyncing, Message: "sync tick"})
			}
			e.Logger.Info("sync tick")
			if e.Status != nil {
				e.Status.Update(status.Snapshot{State: status.StateIdle, Message: "idle"})
			}
		}
	}
}

func (e *Engine) handleEvent(evt fswatch.Event) {
	if e.Status != nil {
		e.Status.Update(status.Snapshot{State: status.StateSyncing, Message: "processing event"})
	}
	e.Logger.Info("fs event", zap.String("path", evt.Path))
	if e.Status != nil {
		e.Status.Update(status.Snapshot{State: status.StateIdle, Message: "idle"})
	}
}
