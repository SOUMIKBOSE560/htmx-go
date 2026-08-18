// Package logs bridges backend logging to live subscribers (SSE). A Hub keeps
// a bounded history of recent log lines and fans new lines out to every open
// subscriber, mirroring the log stream pattern used by the reference app.
package logs

import (
	"context"
	"sync"
)

// Hub is a bounded, thread-safe ring buffer of log lines plus a fan-out of new
// lines to subscribers. Slow subscribers are dropped rather than allowed to
// block the logger.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan string]struct{}
	history []string
	max     int
}

// NewHub returns a Hub that retains at most max recent lines.
func NewHub(max int) *Hub {
	if max < 1 {
		max = 1
	}
	return &Hub{
		subs: make(map[chan string]struct{}),
		max:  max,
	}
}

// Publish appends line to the history and delivers it to every subscriber.
func (h *Hub) Publish(line string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.history = append(h.history, line)
	if len(h.history) > h.max {
		h.history = h.history[len(h.history)-h.max:]
	}
	for ch := range h.subs {
		select {
		case ch <- line:
		default: // subscriber backed up — drop the line for it
		}
	}
}

// History returns a copy of the retained lines, oldest first.
func (h *Hub) History() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.history))
	copy(out, h.history)
	return out
}

// Subscribe registers a new subscriber for the lifetime of ctx. The returned
// channel is closed when ctx is done or the hub is garbage collected.
func (h *Hub) Subscribe(ctx context.Context) <-chan string {
	ch := make(chan string, 128)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.subs, ch)
		close(ch)
		h.mu.Unlock()
	}()
	return ch
}
