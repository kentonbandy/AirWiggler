package server

import (
	"sync"
	"time"
)

// eventCounter is a thread-safe rolling-window event counter.
type eventCounter struct {
	mu     sync.Mutex
	window time.Duration
	events []time.Time
}

func newEventCounter(window time.Duration) *eventCounter {
	return &eventCounter{window: window}
}

// record adds an event at the current time.
func (c *eventCounter) record() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, now)
	c.prune(now)
}

// count returns the number of events within the rolling window.
func (c *eventCounter) count() int {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)
	return len(c.events)
}

// prune removes events outside the window. Must be called with mu held.
func (c *eventCounter) prune(now time.Time) {
	cutoff := now.Add(-c.window)
	i := 0
	for i < len(c.events) && c.events[i].Before(cutoff) {
		i++
	}
	c.events = c.events[i:]
}
