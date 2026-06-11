//go:build windows

package terminal

import (
	"os"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
	"golang.org/x/sys/windows"
)

// setProcessGroupAttr is a no-op on Windows.
// Windows doesn't support Unix-style process groups.
// ConPty handles process lifecycle management automatically.
func setProcessGroupAttr(c *pty.Cmd) {
	// Windows ConPty manages process lifecycle through the pseudo console
	// No need to set process group attributes
}

// killProcessGroup kills the process and its children on Windows.
// On Windows, we use the process handle to terminate the process tree.
func killProcessGroup(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	// On Windows, we can use the process handle to terminate
	// This will also terminate child processes when using ConPty
	handle := windows.Handle(process.Pid)
	if handle != 0 {
		windows.TerminateProcess(handle, 1)
	}

	// Also try the standard Kill method as fallback
	process.Kill()
}

// conptyMu provides mutual exclusion for ConPty kill+spawn operations.
// Prevents rapid kill+spawn deadlocks on Windows (P5-2).
var conptyMu sync.Mutex

// conptyCooldown prevents rapid ConPty kill+spawn cycles.
// After killing a ConPty process, wait 200ms before spawning a new one.
// Aligns with IDE's ConPty cooldown logic.
var conptyCooldown = 200 * time.Millisecond

// drainConPtyOutput drains remaining output from a ConPty before killing.
// Without draining, ConPty can leave zombie processes or leak data (P5-2).
// Timeout: 100ms to avoid blocking shutdown.
func drainConPtyOutput(p pty.Pty) {
	if p == nil {
		return
	}

	buf := make([]byte, 4096)
	deadline := time.After(100 * time.Millisecond)

	for {
		select {
		case <-deadline:
			return
		default:
			n, err := p.Read(buf)
			if err != nil || n == 0 {
				return
			}
		}
	}
}

// conptyInheritCursor controls whether ConPty cursor position is inherited
// on Revive. When true, the revived terminal inherits cursor position from
// serialized state. Aligns with IDE conptyInheritCursor (P5-1).
//
// IDE: conptyInheritCursor: useConpty && !!shellLaunchConfig.initialText
// In axons, we always set this to true when reviving with serialized state
// (which provides initialText).
var conptyInheritCursor = true