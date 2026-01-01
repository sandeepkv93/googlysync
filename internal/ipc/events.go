package ipc

import (
	ipcgen "github.com/sandeepkv93/googlysync/internal/ipc/gen"
	"github.com/sandeepkv93/googlysync/internal/status"
)

func toProtoEvents(events []status.Event) []*ipcgen.StatusEvent {
	out := make([]*ipcgen.StatusEvent, 0, len(events))
	for _, evt := range events {
		out = append(out, &ipcgen.StatusEvent{
			Op:         evt.Op,
			Path:       evt.Path,
			OccurredAt: toProtoTimestamp(evt.When),
		})
	}
	return out
}
