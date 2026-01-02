//go:build wireinject
// +build wireinject

//go:generate go run github.com/google/wire/cmd/wire

package main

import (
	"github.com/google/wire"

	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/daemon"
	"github.com/sandeepkv93/googlysync/internal/fswatch"
	"github.com/sandeepkv93/googlysync/internal/ipc"
	"github.com/sandeepkv93/googlysync/internal/logging"
	"github.com/sandeepkv93/googlysync/internal/storage"
	syncer "github.com/sandeepkv93/googlysync/internal/sync"
)

func InitializeDaemon(opts config.Options) (*daemon.Daemon, error) {
	wire.Build(
		config.NewConfigWithOptions,
		logging.NewLogger,
		storage.NewStorage,
		newStatusStore,
		newAuthService,
		fswatch.NewWatcher,
		newSyncQueue,
		syncer.NewEngine,
		ipc.NewServer,
		daemon.NewDaemon,
	)
	return &daemon.Daemon{}, nil
}
