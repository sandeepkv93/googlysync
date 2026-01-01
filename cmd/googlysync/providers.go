package main

import (
	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/status"
	syncer "github.com/sandeepkv93/googlysync/internal/sync"
)

func newStatusStore(cfg *config.Config) *status.Store {
	store := status.NewStore()
	store.SetMaxEvents(cfg.EventLogSize)
	return store
}

func newSyncQueue(logger *zap.Logger, cfg *config.Config) *syncer.Queue {
	return syncer.NewQueue(logger, cfg.SyncQueueSize)
}
