// Package notification provides the notification system for axons.
package notification

import "time"

// Notification represents a notification message.
type Notification struct {
	ID        string    `json:"id"`         // UUID v4
	Source    string    `json:"source"`     // "host" | pluginId (e.g. "com.axons.huggingface")
	Type      string    `json:"type"`       // info | warning | error | success | progress
	Title     string    `json:"title"`      // notification title
	Message   string    `json:"message"`    // notification body (optional)
	Group     string    `json:"group"`      // group identifier (optional, phase 3)
	Actions   []Action  `json:"actions"`    // optional action buttons (phase 2)
	Read      bool      `json:"read"`       // whether the notification has been read
	Timestamp time.Time `json:"timestamp"`  // creation time
}

// Action represents an interactive button on a notification.
type Action struct {
	ID    string `json:"id"`    // action identifier
	Label string `json:"label"` // button text
	URL   string `json:"url"`   // click target URL (optional, e.g. "panel://huggingface")
}

// ValidNotificationTypes is the set of valid notification types.
var ValidNotificationTypes = map[string]bool{
	"info":     true,
	"success":  true,
	"warning":  true,
	"error":    true,
	"progress": true,
}

// CreateNotificationRequest represents the POST request body for creating a notification.
// Source is NOT included — it is filled by the daemon based on authentication.
type CreateNotificationRequest struct {
	Type    string   `json:"type"`              // defaults to "info" if empty
	Title   string   `json:"title"`             // required, max 200 chars
	Message string   `json:"message"`           // optional, max 1000 chars
	Group   string   `json:"group"`             // optional
	Actions []Action `json:"actions"`            // optional, max 3 items
}

// ListOptions represents the query parameters for listing notifications.
type ListOptions struct {
	Unread bool   // filter by unread status
	Source string // filter by source
	Type   string // filter by type
	Limit  int    // page size (max 100, default 50)
	Offset int    // offset
}

// NotificationListResponse represents the response for listing notifications.
type NotificationListResponse struct {
	Notifications []Notification `json:"notifications"`
	Total         int            `json:"total"`
	UnreadCount   int            `json:"unreadCount"`
}

// UnreadCountResponse represents the response for the unread count endpoint.
type UnreadCountResponse struct {
	Count int `json:"count"`
}