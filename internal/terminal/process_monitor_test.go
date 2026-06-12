package terminal

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewChildProcessMonitor(t *testing.T) {
	m := NewChildProcessMonitor(1234)
	if m == nil {
		t.Fatal("NewChildProcessMonitor returned nil")
	}
	if m.pid != 1234 {
		t.Errorf("expected pid 1234, got %d", m.pid)
	}
}

func TestChildProcessMonitor_HasChildProcesses_Initial(t *testing.T) {
	m := NewChildProcessMonitor(os.Getpid())
	if m.HasChildProcesses() {
		t.Error("expected no child processes initially")
	}
	m.Stop()
}

func TestChildProcessMonitor_SetOnChange(t *testing.T) {
	m := NewChildProcessMonitor(os.Getpid())

	var called atomic.Bool
	m.SetOnChange(func(hasChildProcesses bool) {
		called.Store(true)
	})

	// Trigger a check via OnInput (debounce 1s)
	m.OnInput()

	// Wait for debounce timer to fire
	time.Sleep(1500 * time.Millisecond)

	// The callback may or may not have been called depending on whether
	// the test process has children. We just verify no panic occurred.
	_ = called.Load()

	m.Stop()
}

func TestChildProcessMonitor_OnInput_Debounce(t *testing.T) {
	m := NewChildProcessMonitor(os.Getpid())

	// Rapid calls should reset the debounce timer
	m.OnInput()
	m.OnInput()
	m.OnInput()

	// Stop should clean up without panic
	m.Stop()
}

func TestChildProcessMonitor_OnOutput_Throttle(t *testing.T) {
	m := NewChildProcessMonitor(os.Getpid())

	// First call should schedule a throttle timer
	m.OnOutput()

	// Second call immediately should be throttled (lastCheck was just set)
	m.OnOutput()

	// Stop should clean up without panic
	m.Stop()
}

func TestChildProcessMonitor_Stop(t *testing.T) {
	m := NewChildProcessMonitor(os.Getpid())

	m.OnInput()
	m.OnOutput()

	// Stop should not panic even with active timers
	m.Stop()

	// Double stop should not panic
	m.Stop()
}