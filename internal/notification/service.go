package notification

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mengshi02/axons/internal/plugin"
)

// EventBroadcaster is an interface for broadcasting events (breaks import cycle with internal/api).
type EventBroadcaster interface {
	BroadcastNotification(eventType string, timestamp time.Time, data map[string]interface{})
}

// Service provides notification business logic.
type Service struct {
	repo        *Repository
	broadcaster EventBroadcaster
	pluginMgr   *plugin.Manager
}

// NewService creates a new notification service.
func NewService(repo *Repository, broadcaster EventBroadcaster, pluginMgr *plugin.Manager) *Service {
	return &Service{
		repo:        repo,
		broadcaster: broadcaster,
		pluginMgr:   pluginMgr,
	}
}

// Repository returns the underlying repository (for direct DB access from handlers).
func (s *Service) Repository() *Repository {
	return s.repo
}

// Create creates or updates a notification + SSE broadcast + auto cleanup.
// For progress notifications: if same source+title exists, updates instead of creating.
// For success/error: if same source+title has a progress record, converts it.
func (s *Service) Create(ctx context.Context, n *Notification) error {
	// Generate ID if not set
	if n.ID == "" {
		n.ID = uuid.NewString()
	}

	// Set default type
	if n.Type == "" {
		n.Type = "info"
	}

	// Set timestamp if not set
	if n.Timestamp.IsZero() {
		n.Timestamp = time.Now()
	}

	// Find existing progress notification with same source + title
	existing, _ := s.repo.FindProgressBySourceAndTitle(ctx, n.Source, n.Title)

	if existing != nil {
		// Update existing record (progress update or progress→terminal state)
		if err := s.repo.Update(ctx, existing.ID, n.Message, n.Type); err != nil {
			return err
		}
		n.ID = existing.ID // reuse original ID for frontend update

		s.broadcastNotification(n, "updated")
		return nil
	}

	// No existing progress record → create new record
	if err := s.repo.Create(ctx, n); err != nil {
		return err
	}

	// Cleanup excess notifications (failure doesn't affect creation)
	if err := s.repo.Cleanup(ctx); err != nil {
		log.Printf("notification cleanup failed: %v", err)
	}

	s.broadcastNotification(n, "created")
	return nil
}

// broadcastNotification broadcasts a notification event via EventBroker and PluginEventBus.
func (s *Service) broadcastNotification(n *Notification, action string) {
	payload := map[string]interface{}{
		"action":    action,
		"id":        n.ID,
		"source":    n.Source,
		"type":      n.Type,
		"title":     n.Title,
		"message":   n.Message,
		"actions":   n.Actions,
		"read":      n.Read,
		"timestamp": n.Timestamp,
	}

	// Broadcast to main SSE event stream via the broadcaster interface
	if s.broadcaster != nil {
		s.broadcaster.BroadcastNotification("notification", time.Now(), payload)
	}

	// Also broadcast to PluginEventBus so iframe plugins receive it
	plugin.GetGlobalBus().Emit(plugin.Event{
		PluginID: n.Source,
		Type:     "notification",
		Payload:  payload,
	})
}

// IdentifySource identifies the notification source from the request.
// If Authorization header is present, it looks up the plugin by token.
// If no Authorization header, source defaults to "host".
func (s *Service) IdentifySource(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "host", nil
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == auth {
		return "host", nil // not a Bearer token
	}
	if s.pluginMgr == nil {
		return "", fmt.Errorf("invalid token: plugin manager not available")
	}
	pluginID, ok := s.pluginMgr.FindPluginByToken(token)
	if !ok {
		return "", fmt.Errorf("invalid token")
	}
	return pluginID, nil
}

// HasPermission checks if a plugin has the specified permission.
func (s *Service) HasPermission(pluginID, permission string) bool {
	if s.pluginMgr == nil {
		return false
	}
	return s.pluginMgr.HasPermission(pluginID, permission)
}