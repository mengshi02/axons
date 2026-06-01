package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/notification"
)

// handleGetNotifications handles GET /v1/notifications
func (s *Server) handleGetNotifications(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if s.notificationService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "notification service not available")
		return
	}

	opts := notification.ListOptions{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("unread"); v == "true" || v == "1" {
		opts.Unread = true
	}
	if v := r.URL.Query().Get("source"); v != "" {
		opts.Source = v
	}
	if v := r.URL.Query().Get("type"); v != "" {
		opts.Type = v
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}

	notifications, total, unreadCount, err := s.notificationService.Repository().List(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.internal"))
		return
	}

	writeJSON(w, http.StatusOK, notification.NotificationListResponse{
		Notifications: notifications,
		Total:         total,
		UnreadCount:   unreadCount,
	})
}

// handleCreateNotification handles POST /v1/notifications
func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if s.notificationService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "notification service not available")
		return
	}

	// 1. Identify source
	source, err := s.notificationService.IdentifySource(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}

	// 2. Permission check (plugins need notification:send)
	if source != "host" {
		if !s.notificationService.HasPermission(source, "notification:send") {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "missing notification:send permission")
			return
		}
	}

	// 3. Parse request body
	var req notification.CreateNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// 4. Input validation
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "title is required")
		return
	}
	if len(req.Title) > 200 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "title exceeds 200 characters")
		return
	}
	if len(req.Message) > 1000 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "message exceeds 1000 characters")
		return
	}
	if len(req.Actions) > 3 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "actions exceed maximum of 3")
		return
	}
	for _, a := range req.Actions {
		if len(a.Label) > 50 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("action label '%s' exceeds 50 characters", a.ID))
			return
		}
		if len(a.URL) > 500 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("action url '%s' exceeds 500 characters", a.ID))
			return
		}
	}

	// Validate type
	nType := req.Type
	if nType == "" {
		nType = "info"
	}
	if !notification.ValidNotificationTypes[nType] {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("invalid notification type: %s", nType))
		return
	}

	// 5. Construct Notification (source filled by backend, ignore request body source)
	n := &notification.Notification{
		ID:        uuid.NewString(),
		Source:    source,
		Type:      nType,
		Title:     req.Title,
		Message:   req.Message,
		Group:     req.Group,
		Actions:   req.Actions,
		Read:      false,
		Timestamp: time.Now(),
	}

	// 6. Create + broadcast
	if err := s.notificationService.Create(r.Context(), n); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create notification")
		return
	}

	// 7. Return created notification
	writeJSON(w, http.StatusCreated, n)
}

// handleMarkNotificationRead handles PUT /v1/notifications/:id/read
func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request, id string) {
	if s.notificationService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "notification service not available")
		return
	}

	if err := s.notificationService.Repository().MarkRead(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to mark notification as read")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMarkAllNotificationsRead handles PUT /v1/notifications/read-all
func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if s.notificationService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "notification service not available")
		return
	}

	if err := s.notificationService.Repository().MarkAllRead(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to mark all notifications as read")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteNotification handles DELETE /v1/notifications/:id
func (s *Server) handleDeleteNotification(w http.ResponseWriter, r *http.Request, id string) {
	if s.notificationService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "notification service not available")
		return
	}

	if err := s.notificationService.Repository().Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete notification")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetUnreadCount handles GET /v1/notifications/unread-count
func (s *Server) handleGetUnreadCount(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if s.notificationService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "notification service not available")
		return
	}

	count, err := s.notificationService.Repository().UnreadCount(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get unread count")
		return
	}

	writeJSON(w, http.StatusOK, notification.UnreadCountResponse{Count: count})
}

// handleNotificationDispatch is a catch-all dispatcher for notification sub-routes.
// This avoids httprouter static/param segment conflicts.
func (s *Server) handleNotificationDispatch(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	path := ps.ByName("path")
	method := r.Method

	switch {
	case method == "PUT" && path == "read-all":
		s.handleMarkAllNotificationsRead(w, r, ps)
	case method == "PUT" && strings.HasSuffix(path, "/read"):
		// path = "<id>/read" → extract id
		id := strings.TrimSuffix(path, "/read")
		s.handleMarkNotificationRead(w, r, id)
	case method == "DELETE" && !strings.Contains(path, "/"):
		// path = "<id>"
		s.handleDeleteNotification(w, r, path)
	default:
		http.NotFound(w, r)
	}
}