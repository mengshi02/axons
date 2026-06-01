// Package task provides asynchronous task management.
package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Manager manages asynchronous tasks.
type Manager struct {
	tasks   sync.Map // map[string]*Task
	subs    sync.Map // map[string][]chan Event
	subsMu  sync.RWMutex
	cleanup time.Duration
}

// NewManager creates a new task manager.
func NewManager(cleanupInterval time.Duration) *Manager {
	m := &Manager{
		cleanup: cleanupInterval,
	}
	if cleanupInterval > 0 {
		go m.runCleanup()
	}
	return m
}

// CreateTask creates a new task with the given type.
func (m *Manager) CreateTask(taskType string) *Task {
	ctx, cancel := context.WithCancel(context.Background())
	task := &Task{
		ID:        generateID(),
		Type:      taskType,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}
	m.tasks.Store(task.ID, task)
	return task
}

// GetTask retrieves a task by ID.
func (m *Manager) GetTask(id string) (*Task, bool) {
	v, ok := m.tasks.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*Task), true
}

// DeleteTask removes a task by ID.
func (m *Manager) DeleteTask(id string) {
	m.tasks.Delete(id)
	// Also clean up subscribers
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	if subs, ok := m.subs.Load(id); ok {
		m.subs.Delete(id)
		for _, ch := range subs.([]chan Event) {
			close(ch)
		}
	}
}

// ListTasks returns all tasks.
func (m *Manager) ListTasks() []*Task {
	var tasks []*Task
	m.tasks.Range(func(key, value interface{}) bool {
		tasks = append(tasks, value.(*Task))
		return true
	})
	return tasks
}

// StartTask marks a task as running.
func (m *Manager) StartTask(id string) error {
	task, ok := m.GetTask(id)
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	task.SetStatus(StatusRunning)
	m.emitEvent(id, Event{
		TaskID:    id,
		Type:      EventProgress,
		Timestamp: time.Now(),
	})
	return nil
}

// CompleteTask marks a task as complete with a result.
func (m *Manager) CompleteTask(id string, result any) {
	task, ok := m.GetTask(id)
	if !ok {
		return
	}
	task.SetResult(result)
	m.emitEvent(id, Event{
		TaskID:    id,
		Type:      EventComplete,
		Timestamp: time.Now(),
	})
}

// FailTask marks a task as failed with an error.
func (m *Manager) FailTask(id string, err error) {
	task, ok := m.GetTask(id)
	if !ok {
		return
	}
	task.SetError(err.Error())
	m.emitEvent(id, Event{
		TaskID:    id,
		Type:      EventError,
		Message:   err.Error(),
		Timestamp: time.Now(),
	})
}

// CancelTask cancels a task.
func (m *Manager) CancelTask(id string) error {
	task, ok := m.GetTask(id)
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	task.Cancel()
	m.emitEvent(id, Event{
		TaskID:    id,
		Type:      EventCancel,
		Timestamp: time.Now(),
	})
	return nil
}

// UpdateProgress updates task progress.
func (m *Manager) UpdateProgress(id string, progress, total int, message string) {
	task, ok := m.GetTask(id)
	if !ok {
		return
	}
	task.SetProgress(progress, total, message)
	m.emitEvent(id, Event{
		TaskID:    id,
		Type:      EventProgress,
		Progress:  progress,
		Total:     total,
		Message:   message,
		Timestamp: time.Now(),
	})
}

// Subscribe subscribes to task events.
func (m *Manager) Subscribe(id string, ch chan Event) error {
	if _, ok := m.GetTask(id); !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	m.subsMu.Lock()
	defer m.subsMu.Unlock()

	subs, _ := m.subs.LoadOrStore(id, []chan Event{})
	subList := subs.([]chan Event)
	m.subs.Store(id, append(subList, ch))

	return nil
}

// Unsubscribe removes a subscriber.
func (m *Manager) Unsubscribe(id string, ch chan Event) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()

	if subs, ok := m.subs.Load(id); ok {
		subList := subs.([]chan Event)
		for i, sub := range subList {
			if sub == ch {
				m.subs.Store(id, append(subList[:i], subList[i+1:]...))
				break
			}
		}
	}
}

// emitEvent sends an event to all subscribers.
func (m *Manager) emitEvent(id string, event Event) {
	m.subsMu.RLock()
	defer m.subsMu.RUnlock()

	if subs, ok := m.subs.Load(id); ok {
		for _, ch := range subs.([]chan Event) {
			select {
			case ch <- event:
			default:
				// Channel full, skip
			}
		}
	}
}

// runCleanup periodically removes completed/failed tasks.
func (m *Manager) runCleanup() {
	ticker := time.NewTicker(m.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		m.tasks.Range(func(key, value interface{}) bool {
			task := value.(*Task)
			if task.Status == StatusComplete || task.Status == StatusError || task.Status == StatusCanceled {
				// Delete completed tasks older than 1 hour
				if time.Since(task.UpdatedAt) > time.Hour {
					m.DeleteTask(key.(string))
				}
			}
			return true
		})
	}
}

// generateID generates a random task ID.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Items represents a list of tasks.
type Items struct {
	Tasks []*Task `json:"tasks"`
	Count int     `json:"count"`
}