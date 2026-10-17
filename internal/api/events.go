package api

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is one committed change streamed to the console over SSE.
type Event struct {
	Kind    string `json:"kind"`
	At      string `json:"at"`
	Payload any    `json:"payload"`
}

// Hub fans committed kernel events out to SSE subscribers. Slow subscribers
// drop events rather than block the kernel; the console refetches on gaps.
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

func (h *Hub) Publish(kind string, payload any) {
	b, err := json.Marshal(Event{Kind: kind, At: time.Now().UTC().Format(time.RFC3339Nano), Payload: payload})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

func (h *Hub) subscribe() chan []byte {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}
