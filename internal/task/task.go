// Package task provides asynchronous task management.
package task

import (
	"context"
	"sync"
	"time"
)

// Status represents the current state of a task.
type Status string

const (
	// StatusPending means the task is waiting to be executed.
	StatusPending Status = "pending"
	// StatusRunning means the task is currently being executed.
	StatusRunning Status = "running"
	// StatusComplete means the task completed successfully.
	StatusComplete Status = "complete"
	// StatusError means the task failed with an error.
	StatusError Status = "error"
	// StatusCanceled means the task was canceled by the user.
	StatusCanceled Status = "canceled"
)

// Task represents an asynchronous operation.
type Task struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    Status    `json:"status"`
	Progress  int       `json:"progress"`
	Total     int       `json:"total"`
	Message   string    `json:"message"`
	Error     string    `json:"error,omitempty"`
	Result    any       `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Labels    Map       `json:"labels,omitempty"`

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// Map is a thread-safe string map for task labels/metadata.
type Map map[string]string

// Context returns the task's context.
func (t *Task) Context() context.Context {
	return t.ctx
}

// Cancel cancels the task.
func (t *Task) Cancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil && t.Status == StatusRunning {
		t.cancel()
		t.Status = StatusCanceled
		t.UpdatedAt = time.Now()
	}
}

// SetProgress updates the task progress.
func (t *Task) SetProgress(progress, total int, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Progress = progress
	t.Total = total
	t.Message = message
	t.UpdatedAt = time.Now()
}

// SetStatus updates the task status.
func (t *Task) SetStatus(status Status) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Status = status
	t.UpdatedAt = time.Now()
}

// SetError sets the task error.
func (t *Task) SetError(err string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Status = StatusError
	t.Error = err
	t.UpdatedAt = time.Now()
}

// SetResult sets the task result.
func (t *Task) SetResult(result any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Status = StatusComplete
	t.Result = result
	t.UpdatedAt = time.Now()
}

// IsCanceled returns true if the task context is canceled.
func (t *Task) IsCanceled() bool {
	select {
	case <-t.ctx.Done():
		return true
	default:
		return false
	}
}

// Event represents a task event for progress streaming.
type Event struct {
	TaskID    string    `json:"task_id"`
	Type      string    `json:"type"`
	Progress  int       `json:"progress"`
	Total     int       `json:"total"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// TaskStatus is a serializable representation of a task's status.
type TaskStatus struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    Status    `json:"status"`
	Progress  int       `json:"progress"`
	Total     int       `json:"total"`
	Message   string    `json:"message"`
	Error     string    `json:"error,omitempty"`
	Result    any       `json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Labels    Map       `json:"labels,omitempty"`
}

// ToStatus returns a TaskStatus representation of the task.
func (t *Task) ToStatus() TaskStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return TaskStatus{
		ID:        t.ID,
		Type:      t.Type,
		Status:    t.Status,
		Progress:  t.Progress,
		Total:     t.Total,
		Message:   t.Message,
		Error:     t.Error,
		Result:    t.Result,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
		Labels:    t.Labels,
	}
}

// Event types.
const (
	EventProgress = "progress"
	EventComplete = "complete"
	EventError    = "error"
	EventCancel   = "cancel"
)