package status

import (
	"sync"
	"time"
)

// State describes high-level sync state.
type State int

const (
	StateUnspecified State = iota
	StateIdle
	StateSyncing
	StateError
	StatePaused
)

// Event captures a recent filesystem event.
type Event struct {
	Op   string
	Path string
	When time.Time
}

// Snapshot captures current status.
type Snapshot struct {
	State        State
	Message      string
	LastEvent    string
	UpdatedAt    time.Time
	RecentEvents []Event
}

// Store holds the latest status snapshot.
type Store struct {
	mu        sync.Mutex
	snapshot  Snapshot
	maxEvents int
	eventRing []Event
}

// NewStore constructs a status store with an initial idle state.
func NewStore() *Store {
	s := &Store{maxEvents: 20}
	s.snapshot = Snapshot{State: StateIdle, Message: "idle", UpdatedAt: time.Now()}
	return s
}

// SetMaxEvents sets the max number of events to retain.
func (s *Store) SetMaxEvents(max int) {
	if max <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.maxEvents = max
	if len(s.eventRing) > max {
		s.eventRing = s.eventRing[len(s.eventRing)-max:]
	}
	s.snapshot.RecentEvents = append([]Event(nil), s.eventRing...)
}

// Update replaces the current snapshot, preserving LastEvent when omitted.
func (s *Store) Update(snapshot Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = time.Now()
	}
	if snapshot.LastEvent == "" {
		snapshot.LastEvent = s.snapshot.LastEvent
	}
	snapshot.RecentEvents = append([]Event(nil), s.eventRing...)
	s.snapshot = snapshot
}

// AddEvent appends a recent event and updates LastEvent.
func (s *Store) AddEvent(evt Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if evt.When.IsZero() {
		evt.When = time.Now()
	}

	s.eventRing = append(s.eventRing, evt)
	if len(s.eventRing) > s.maxEvents {
		s.eventRing = s.eventRing[len(s.eventRing)-s.maxEvents:]
	}

	s.snapshot.LastEvent = evt.Op + " " + evt.Path
	s.snapshot.UpdatedAt = time.Now()
	s.snapshot.RecentEvents = append([]Event(nil), s.eventRing...)
}

// Current returns a copy of the latest snapshot.
func (s *Store) Current() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	copySnapshot := s.snapshot
	copySnapshot.RecentEvents = append([]Event(nil), s.eventRing...)
	return copySnapshot
}
