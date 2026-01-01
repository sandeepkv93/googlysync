package sync

import (
	"sync"

	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/fswatch"
)

// Queue buffers filesystem events for processing.
type Queue struct {
	logger *zap.Logger
	mu     sync.Mutex
	ch     chan fswatch.Event
}

// NewQueue constructs a queue with the given capacity.
func NewQueue(logger *zap.Logger, capacity int) *Queue {
	if capacity <= 0 {
		capacity = 1024
	}
	return &Queue{logger: logger, ch: make(chan fswatch.Event, capacity)}
}

// Enqueue adds an event to the queue.
func (q *Queue) Enqueue(evt fswatch.Event) {
	select {
	case q.ch <- evt:
	default:
		q.logger.Warn("sync queue full; dropping event", zap.String("path", evt.Path))
	}
}

// Channel returns a receive-only channel for events.
func (q *Queue) Channel() <-chan fswatch.Event {
	return q.ch
}
