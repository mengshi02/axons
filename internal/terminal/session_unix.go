//go:build !windows

package terminal

import (
	"syscall"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// setProcessGroupAttr sets the process group attribute for Unix systems.
// This allows us to kill the entire process group when terminating the session.
// Note: On macOS, setting Setpgid can cause "operation not permitted" errors
// due to security restrictions, so we keep it minimal.
func setProcessGroupAttr(c *pty.Cmd) {
	// Don't set Setpgid on macOS to avoid permission errors
	// The process will still be killed via PID when closing
}

// killProcessGroup kills the process group for the given PID.
// This ensures all child processes are terminated.
func killProcessGroup(pid int) {
	// Try to kill the process group first
	pgid, err := syscall.Getpgid(pid)
	if err == nil && pgid > 0 {
		// Kill the entire process group
		syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(50 * time.Millisecond)
		syscall.Kill(-pgid, syscall.SIGKILL)
	}
	
	// Also try to kill the process directly
	syscall.Kill(pid, syscall.SIGTERM)
	time.Sleep(50 * time.Millisecond)
	syscall.Kill(pid, syscall.SIGKILL)
}



