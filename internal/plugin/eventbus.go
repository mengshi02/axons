package plugin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// PluginEventBus — Go in-memory EventBus, high-performance broadcast center.
// All event emission goes through POST /v1/plugins/event, and all subscriptions
// go through SSE /v1/plugins/events/stream. This replaces the previous frontend
// JS event relay with a Go backend implementation.
type PluginEventBus struct {
	mu    sync.RWMutex
	sinks map[chan Event]struct{} // SSE subscribers
}

// Event represents a plugin event broadcast through the EventBus.
type Event struct {
	PluginID string `json:"pluginId"`
	Type     string `json:"type"`
	Payload  any    `json:"payload"`
}

// globalBus is the singleton EventBus instance.
var globalBus = &PluginEventBus{
	sinks: make(map[chan Event]struct{}),
}

// GetGlobalBus returns the global EventBus instance.
func GetGlobalBus() *PluginEventBus {
	return globalBus
}

// HandlePostEvent handles POST /v1/plugins/event.
// It receives events from builtin panels or iframes and broadcasts to all SSE subscribers.
func (b *PluginEventBus) HandlePostEvent(w http.ResponseWriter, r *http.Request) {
	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	b.broadcast(event)
	w.WriteHeader(http.StatusNoContent)
}

// HandleEventStream handles SSE /v1/plugins/events/stream.
// Both builtin panels and iframes subscribe to events through this endpoint.
func (b *PluginEventBus) HandleEventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := make(chan Event, 64)
	b.register(ch)
	defer b.unregister(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for {
		select {
		case event := <-ch:
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// Emit allows the Go backend to emit events into the bus (e.g., plugin lifecycle events).
func (b *PluginEventBus) Emit(event Event) {
	b.broadcast(event)
}

func (b *PluginEventBus) broadcast(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.sinks {
		select {
		case ch <- event:
		default:
			// Skip slow consumers to avoid blocking broadcast
		}
	}
}

func (b *PluginEventBus) register(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sinks[ch] = struct{}{}
}

func (b *PluginEventBus) unregister(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sinks, ch)
	close(ch)
}