// Package api provides SSE event broadcasting for real-time notifications.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
)

// EventType represents the type of SSE event.
type EventType string

const (
	// EventFileChange is sent when a file is modified/added/deleted
	EventFileChange EventType = "file_change"
	// EventBuildProgress is sent during build operations
	EventBuildProgress EventType = "build_progress"
	// EventBuildComplete is sent when a build finishes
	EventBuildComplete EventType = "build_complete"
	// EventBuildError is sent when a build fails
	EventBuildError EventType = "build_error"
	// EventSearchStep is sent during AI search pipeline
	EventSearchStep EventType = "search_step"
	// EventRAGChunk is sent during RAG streaming output
	EventRAGChunk EventType = "rag_chunk"
	// EventWatchStatus is sent when watch status changes
	EventWatchStatus EventType = "watch_status"
	// EventEmbedProgress is sent during embedding operations
	EventEmbedProgress EventType = "embed_progress"
	// EventEmbedComplete is sent when embedding finishes
	EventEmbedComplete EventType = "embed_complete"
	// EventEmbedError is sent when embedding fails
	EventEmbedError EventType = "embed_error"
	// EventConfigChange is sent when configuration changes
	EventConfigChange EventType = "config_change"

	// Plugin lifecycle events
	// EventPluginStarted is sent when a plugin backend starts successfully
	EventPluginStarted EventType = "plugin.started"
	// EventPluginStopped is sent when a plugin is stopped
	EventPluginStopped EventType = "plugin.stopped"
	// EventPluginCrashed is sent when a plugin crashes and exceeds restart limit
	EventPluginCrashed EventType = "plugin.crashed"
	// EventPluginInstalled is sent when a plugin install completes
	EventPluginInstalled EventType = "plugin.installed"
	// EventPluginInstallProgress is sent during plugin installation
	EventPluginInstallProgress EventType = "plugin.installProgress"
	// EventPluginInstallFailed is sent when plugin install fails
	EventPluginInstallFailed EventType = "plugin.installFailed"
	// EventPluginImported is sent when a plugin package is imported
	EventPluginImported EventType = "plugin.imported"
	// EventPluginUninstalled is sent when a plugin is uninstalled
	EventPluginUninstalled EventType = "plugin.uninstalled"
	// EventPluginCleaned is sent when plugin residual data is cleaned up
	EventPluginCleaned EventType = "plugin.cleaned"

	// EventNotification is sent when a new notification is created or updated
	EventNotification EventType = "notification"
	// EventBuildDelta is sent during build when intermediate graph data is available
	EventBuildDelta EventType = "build_delta"
)

// Event represents an SSE event.
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// EventBroker manages SSE connections and broadcasts events.
type EventBroker struct {
	mu         sync.RWMutex
	clients    map[chan Event]struct{}
	broadcast  chan Event
	register   chan chan Event
	unregister chan chan Event
	stop       chan struct{}
}

// NewEventBroker creates a new EventBroker.
func NewEventBroker() *EventBroker {
	b := &EventBroker{
		clients:    make(map[chan Event]struct{}),
		broadcast:  make(chan Event, 100),
		register:   make(chan chan Event),
		unregister: make(chan chan Event),
		stop:       make(chan struct{}),
	}
	go b.run()
	return b
}

// run handles client registration and event broadcasting.
func (b *EventBroker) run() {
	for {
		select {
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client] = struct{}{}
			b.mu.Unlock()

		case client := <-b.unregister:
			b.mu.Lock()
			delete(b.clients, client)
			close(client)
			b.mu.Unlock()

		case event := <-b.broadcast:
			b.mu.RLock()
			for client := range b.clients {
				select {
				case client <- event:
				default:
					// Client buffer full, skip
				}
			}
			b.mu.RUnlock()

		case <-b.stop:
			b.mu.Lock()
			for client := range b.clients {
				close(client)
			}
			b.clients = make(map[chan Event]struct{})
			b.mu.Unlock()
			return
		}
	}
}

// Subscribe creates a new client channel for receiving events.
func (b *EventBroker) Subscribe() chan Event {
	client := make(chan Event, 10)
	b.register <- client
	return client
}

// Unsubscribe removes a client from the broker.
func (b *EventBroker) Unsubscribe(client chan Event) {
	b.unregister <- client
}

// Broadcast sends an event to all connected clients.
func (b *EventBroker) Broadcast(event Event) {
	select {
	case b.broadcast <- event:
	default:
		// Channel full, drop event
	}
}

// BroadcastNotification implements notification.EventBroadcaster interface.
// It broadcasts a notification event with the given type, timestamp and data.
func (b *EventBroker) BroadcastNotification(eventType string, timestamp time.Time, data map[string]interface{}) {
	b.Broadcast(Event{
		Type:      EventType(eventType),
		Timestamp: timestamp,
		Data:      data,
	})
}

// BroadcastFileChange broadcasts a file change event.
func (b *EventBroker) BroadcastFileChange(projectID string, filePath, changeType string) {
	b.Broadcast(Event{
		Type:      EventFileChange,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"project_id":  projectID,
			"file_path":   filePath,
			"change_type": changeType,
		},
	})
}

// BroadcastBuildProgress broadcasts a build progress event.
func (b *EventBroker) BroadcastBuildProgress(taskID string, progress int, message string, projectID string, phase string) {
	b.Broadcast(Event{
		Type:      EventBuildProgress,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"task_id":     taskID,
			"progress":    progress,
			"message":     message,
			"phase":       phase,
			"project_id":  projectID,
		},
	})
}

// BroadcastBuildComplete broadcasts a build completion event.
func (b *EventBroker) BroadcastBuildComplete(taskID string, projectID string, filesParsed, nodesCreated, edgesCreated int, changedFiles, removedFiles []string, changedFileOldNodeIDs, changedFileOldEdgeIDs []int64) {
	data := map[string]interface{}{
		"task_id":       taskID,
		"project_id":    projectID,
		"files_parsed":  filesParsed,
		"nodes_created": nodesCreated,
		"edges_created": edgesCreated,
	}
	if len(changedFiles) > 0 {
		data["changed_files"] = changedFiles
	}
	if len(removedFiles) > 0 {
		data["removed_files"] = removedFiles
	}
	if len(changedFileOldNodeIDs) > 0 {
		// Convert int64 to string for JSON compatibility with frontend
		oldNodeIDStrs := make([]string, len(changedFileOldNodeIDs))
		for i, id := range changedFileOldNodeIDs {
			oldNodeIDStrs[i] = strconv.FormatInt(id, 10)
		}
		data["changed_file_old_node_ids"] = oldNodeIDStrs
	}
	if len(changedFileOldEdgeIDs) > 0 {
		oldEdgeIDStrs := make([]string, len(changedFileOldEdgeIDs))
		for i, id := range changedFileOldEdgeIDs {
			oldEdgeIDStrs[i] = strconv.FormatInt(id, 10)
		}
		data["changed_file_old_edge_ids"] = oldEdgeIDStrs
	}
	b.Broadcast(Event{
		Type:      EventBuildComplete,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// BroadcastBuildDelta broadcasts an intermediate build delta event.
// This allows the frontend to progressively render graph data as each pipeline
// stage completes (e.g., nodes after InsertNodes, edges after BuildEdges).
func (b *EventBroker) BroadcastBuildDelta(taskID string, projectID string, stage string, nodes []map[string]interface{}, edges []map[string]interface{}) {
	data := map[string]interface{}{
		"task_id":    taskID,
		"project_id": projectID,
		"stage":      stage,
	}
	if len(nodes) > 0 {
		data["added_nodes"] = nodes
	}
	if len(edges) > 0 {
		data["added_edges"] = edges
	}
	b.Broadcast(Event{
		Type:      EventBuildDelta,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// BroadcastBuildError broadcasts a build error event.
func (b *EventBroker) BroadcastBuildError(taskID string, projectID string, errorMsg string) {
	b.Broadcast(Event{
		Type:      EventBuildError,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"task_id":    taskID,
			"project_id": projectID,
			"error":      errorMsg,
		},
	})
}

// BroadcastSearchStep broadcasts a search pipeline step event.
func (b *EventBroker) BroadcastSearchStep(queryID, step, status string, durationMs int64, data map[string]interface{}) {
	eventData := map[string]interface{}{
		"query_id":    queryID,
		"step":        step,
		"status":      status,
		"duration_ms": durationMs,
	}
	for k, v := range data {
		eventData[k] = v
	}
	b.Broadcast(Event{
		Type:      EventSearchStep,
		Timestamp: time.Now(),
		Data:      eventData,
	})
}

// BroadcastRAGChunk broadcasts a RAG streaming chunk.
func (b *EventBroker) BroadcastRAGChunk(queryID, content string, done bool) {
	b.Broadcast(Event{
		Type:      EventRAGChunk,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"query_id": queryID,
			"content":  content,
			"done":     done,
		},
	})
}

// BroadcastWatchStatus broadcasts a watch status change event.
func (b *EventBroker) BroadcastWatchStatus(projectID string, status, rootDir string) {
	b.Broadcast(Event{
		Type:      EventWatchStatus,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"project_id": projectID,
			"status":     status,
			"root_dir":   rootDir,
		},
	})
}

// BroadcastEmbedProgress broadcasts an embedding progress event.
func (b *EventBroker) BroadcastEmbedProgress(taskID string, projectID string, current, total int, status string) {
	b.Broadcast(Event{
		Type:      EventEmbedProgress,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"task_id":    taskID,
			"project_id": projectID,
			"current":    current,
			"total":      total,
			"status":     status,
		},
	})
}

// BroadcastEmbedComplete broadcasts an embedding completion event.
func (b *EventBroker) BroadcastEmbedComplete(taskID string, projectID string, totalNodes, newEmbeddings, updatedEmbeddings int) {
	b.Broadcast(Event{
		Type:      EventEmbedComplete,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"task_id":           taskID,
			"project_id":        projectID,
			"total_nodes":       totalNodes,
			"new_embeddings":    newEmbeddings,
			"updated_embeddings": updatedEmbeddings,
		},
	})
}

// BroadcastEmbedError broadcasts an embedding error event.
func (b *EventBroker) BroadcastEmbedError(taskID string, projectID string, errorMsg string) {
	b.Broadcast(Event{
		Type:      EventEmbedError,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"task_id":    taskID,
			"project_id": projectID,
			"error":      errorMsg,
		},
	})
}

// BroadcastConfigChange broadcasts a configuration change event.
func (b *EventBroker) BroadcastConfigChange(configType, message string) {
	b.Broadcast(Event{
		Type:      EventConfigChange,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"config_type": configType,
			"message":     message,
		},
	})
}

// Stop stops the event broker.
func (b *EventBroker) Stop() {
	close(b.stop)
}

// FormatSSE formats an event for SSE transmission.
func (e Event) FormatSSE() string {
	data, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Type, string(data))
}

// handleEvents handles SSE connections for real-time events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Flush headers
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Subscribe to events
	client := s.eventBroker.Subscribe()
	defer s.eventBroker.Unsubscribe(client)

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"message\":\"Connected to event stream\"}\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Heartbeat ticker
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case event, ok := <-client:
			if !ok {
				return
			}
			fmt.Fprintf(w, "%s", event.FormatSSE())
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case <-heartbeat.C:
			// Send heartbeat to keep connection alive
			fmt.Fprintf(w, "event: heartbeat\ndata: {\"time\":%d}\n\n", time.Now().Unix())
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case <-r.Context().Done():
			// Client disconnected
			return
		}
	}
}