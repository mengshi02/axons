// Package terminal provides PTY-based terminal sessions for web terminal feature.
// process_monitor.go implements ChildProcessMonitor for detecting child processes
// of terminal sessions. Aligns with IDE childProcessMonitor.ts.
package terminal

import (
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ChildProcessMonitor monitors whether a terminal session has child processes.
// It uses platform-specific methods to enumerate child processes.
// Aligns with IDE ChildProcessMonitor: input debounce 1s, output throttle 5s.
type ChildProcessMonitor struct {
	pid           int
	hasChildren   atomic.Bool
	lastCheck     atomic.Int64 // ms timestamp of last check
	mu            sync.Mutex
	debounceTimer *time.Timer
	throttleTimer *time.Timer
	onChange      func(hasChildProcesses bool)
}

// NewChildProcessMonitor creates a monitor for the given session PID.
func NewChildProcessMonitor(pid int) *ChildProcessMonitor {
	return &ChildProcessMonitor{
		pid: pid,
	}
}

// SetOnChange sets the callback when hasChildProcesses changes.
func (m *ChildProcessMonitor) SetOnChange(fn func(hasChildProcesses bool)) {
	m.onChange = fn
}

// OnInput triggers a debounce check (1s after user input).
// Aligns with IDE: scheduleCheck() called on user input with debounce 1s.
func (m *ChildProcessMonitor) OnInput() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}
	m.debounceTimer = time.AfterFunc(1*time.Second, func() {
		m.check()
	})
}

// OnOutput triggers a throttle check (5s after PTY output).
// Aligns with IDE: scheduleCheck() called on PTY output with throttle 5s.
func (m *ChildProcessMonitor) OnOutput() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Throttle: only check if last check was more than 5s ago
	last := m.lastCheck.Load()
	if time.Since(time.UnixMilli(last)) < 5*time.Second {
		return
	}

	if m.throttleTimer != nil {
		m.throttleTimer.Stop()
	}
	m.throttleTimer = time.AfterFunc(5*time.Second, func() {
		m.check()
	})
}

// check performs the actual child process enumeration.
func (m *ChildProcessMonitor) check() {
	now := time.Now().UnixMilli()
	m.lastCheck.Store(now)

	has := hasChildProcesses(m.pid)
	previous := m.hasChildren.Load()

	if has != previous {
		m.hasChildren.Store(has)
		zap.L().Debug("ChildProcessMonitor: hasChildProcesses changed",
			zap.Int("pid", m.pid),
			zap.Bool("hasChildren", has))

		if m.onChange != nil {
			m.onChange(has)
		}
	}
}

// HasChildProcesses returns whether the session currently has child processes.
func (m *ChildProcessMonitor) HasChildProcesses() bool {
	return m.hasChildren.Load()
}

// Stop cleans up the monitor timers.
func (m *ChildProcessMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}
	if m.throttleTimer != nil {
		m.throttleTimer.Stop()
	}
}

// hasChildProcesses checks if the given PID has any child processes.
// Platform-specific implementation.
func hasChildProcesses(pid int) bool {
	switch runtime.GOOS {
	case "linux":
		return hasChildProcessesLinux(pid)
	case "darwin":
		return hasChildProcessesDarwin(pid)
	case "windows":
		return hasChildProcessesWindows(pid)
	default:
		// Fallback: assume no child processes on unknown platforms
		return false
	}
}

// hasChildProcessesLinux reads /proc/{pid}/stat to check for child processes.
func hasChildProcessesLinux(pid int) bool {
	// Count entries in /proc that have our PID as ppid
	dir, err := os.Open("/proc")
	if err != nil {
		return false
	}
	defer dir.Close()

	entries, err := dir.Readdirnames(0)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		// Skip non-numeric entries
		if entry[0] < '0' || entry[0] > '9' {
			continue
		}

		// Read /proc/{entry}/stat
		statPath := "/proc/" + entry + "/stat"
		data, err := os.ReadFile(statPath)
		if err != nil {
			continue
		}

		// Parse ppid from stat (field 4 after comm)
		// Format: pid (comm) state ppid ...
		// Find closing parenthesis, then parse fields
		closeParen := -1
		for i, b := range data {
			if b == ')' {
				closeParen = i
				break
			}
		}
		if closeParen < 0 {
			continue
		}

		// Fields after (comm): state ppid pgrp session tty_nr tpgid ...
		fields := make([]int, 0, 7)
		start := closeParen + 2
		for i := start; i < len(data); i++ {
			if data[i] == ' ' {
				continue
			}
			numStart := i
			for i < len(data) && data[i] != ' ' {
				i++
			}
			// Parse number
			val := 0
			for j := numStart; j < i; j++ {
				if data[j] >= '0' && data[j] <= '9' {
					val = val*10 + int(data[j]-'0')
				}
			}
			fields = append(fields, val)
			if len(fields) >= 7 {
				break
			}
		}

		// ppid is field index 1 (0=state, 1=ppid)
		if len(fields) >= 2 && fields[1] == pid {
			return true
		}
	}

	return false
}
