package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Repository provides SQLite CRUD operations for notifications.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a new notification repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Create inserts a new notification.
func (r *Repository) Create(ctx context.Context, n *Notification) error {
	actionsJSON, _ := json.Marshal(n.Actions)
	groupJSON := n.Group
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notifications (id, source, type, title, message, actions, "group", read, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Source, n.Type, n.Title, n.Message, string(actionsJSON), groupJSON, n.Read, n.Timestamp,
	)
	return err
}

// FindProgressBySourceAndTitle finds an existing progress notification with the same source + title.
func (r *Repository) FindProgressBySourceAndTitle(ctx context.Context, source, title string) (*Notification, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, source, type, title, message, actions, "group", read, timestamp
		FROM notifications
		WHERE source = ? AND title = ? AND type = 'progress'
		ORDER BY timestamp DESC LIMIT 1`, source, title)
	return r.scanNotification(row)
}

// Update updates a notification's message, type, and timestamp.
func (r *Repository) Update(ctx context.Context, id, message, nType string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET message = ?, type = ?, timestamp = CURRENT_TIMESTAMP
		WHERE id = ?`, message, nType, id)
	return err
}

// List queries notifications with optional filters.
func (r *Repository) List(ctx context.Context, opts ListOptions) ([]Notification, int, int, error) {
	var whereClauses []string
	var args []interface{}

	if opts.Unread {
		whereClauses = append(whereClauses, "read = 0")
	}
	if opts.Source != "" {
		whereClauses = append(whereClauses, "source = ?")
		args = append(args, opts.Source)
	}
	if opts.Type != "" {
		whereClauses = append(whereClauses, "type = ?")
		args = append(args, opts.Type)
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Get total count
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM notifications %s", whereClause)
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to count notifications: %w", err)
	}

	// Get unread count (always total unread, not filtered)
	var unreadCount int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE read = 0").Scan(&unreadCount); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to count unread notifications: %w", err)
	}

	// Apply limit/offset
	limit := opts.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	listSQL := fmt.Sprintf(`
		SELECT id, source, type, title, message, actions, "group", read, timestamp
		FROM notifications %s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?`, whereClause)
	listArgs := append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to list notifications: %w", err)
	}
	defer rows.Close()

	var notifications []Notification
	for rows.Next() {
		n, err := r.scanNotificationFromRow(rows)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to scan notification: %w", err)
		}
		notifications = append(notifications, *n)
	}

	// Ensure non-nil slice for JSON serialization (null vs [])
	if notifications == nil {
		notifications = []Notification{}
	}

	return notifications, total, unreadCount, nil
}

// MarkRead marks a single notification as read.
func (r *Repository) MarkRead(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE notifications SET read = 1 WHERE id = ?`, id)
	return err
}

// MarkAllRead marks all notifications as read.
func (r *Repository) MarkAllRead(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE notifications SET read = 1 WHERE read = 0`)
	return err
}

// Delete deletes a single notification.
func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM notifications WHERE id = ?`, id)
	return err
}

// UnreadCount returns the number of unread notifications.
func (r *Repository) UnreadCount(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE read = 0`).Scan(&count)
	return count, err
}

// Cleanup removes excess notifications beyond the capacity limits.
// Two-level cleanup: read soft cap (150) + total hard cap (500).
func (r *Repository) Cleanup(ctx context.Context) error {
	// 1. Delete read notifications beyond the soft cap of 150
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM notifications
		WHERE read = 1 AND id NOT IN (
			SELECT id FROM notifications WHERE read = 1
			ORDER BY timestamp DESC LIMIT 150
		)`); err != nil {
		return fmt.Errorf("failed to cleanup read notifications: %w", err)
	}

	// 2. Delete oldest notifications beyond hard cap of 500 (keep 450)
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM notifications
		WHERE id NOT IN (
			SELECT id FROM notifications
			ORDER BY timestamp DESC LIMIT 450
		)`); err != nil {
		return fmt.Errorf("failed to cleanup total notifications: %w", err)
	}

	return nil
}

// scanNotification scans a single notification from a query row.
func (r *Repository) scanNotification(row *sql.Row) (*Notification, error) {
	var n Notification
	var actionsJSON string
	var readInt int
	if err := row.Scan(&n.ID, &n.Source, &n.Type, &n.Title, &n.Message, &actionsJSON, &n.Group, &readInt, &n.Timestamp); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	n.Read = readInt == 1
	if err := json.Unmarshal([]byte(actionsJSON), &n.Actions); err != nil {
		n.Actions = nil // ignore malformed actions
	}
	if n.Actions == nil {
		n.Actions = []Action{} // ensure non-nil for JSON serialization
	}
	return &n, nil
}

// scanNotificationFromRow scans a single notification from query rows.
func (r *Repository) scanNotificationFromRow(rows *sql.Rows) (*Notification, error) {
	var n Notification
	var actionsJSON string
	var readInt int
	if err := rows.Scan(&n.ID, &n.Source, &n.Type, &n.Title, &n.Message, &actionsJSON, &n.Group, &readInt, &n.Timestamp); err != nil {
		return nil, err
	}
	n.Read = readInt == 1
	if err := json.Unmarshal([]byte(actionsJSON), &n.Actions); err != nil {
		n.Actions = nil
	}
	if n.Actions == nil {
		n.Actions = []Action{}
	}
	return &n, nil
}