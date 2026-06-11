//go:build !windows && !darwin

package terminal

// hasChildProcessesWindows is a stub for non-Windows platforms.
func hasChildProcessesWindows(pid int) bool { return false }

// hasChildProcessesDarwin is a stub for non-Darwin platforms.
func hasChildProcessesDarwin(pid int) bool { return false }