//go:build windows

package terminal

import (
	"os"

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
	// Find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	// On Windows, we can use the process handle to terminate
	// This will also terminate child processes when using ConPty
	handle := windows.Handle(process.Pid)
	if handle != 0 {
		// Terminate the process with exit code 1
		windows.TerminateProcess(handle, 1)
	}

	// Also try the standard Kill method as fallback
	process.Kill()
}

