//go:build windows

package terminal

// hasChildProcessesDarwin is a stub on Windows (never called, needed for compilation).
func hasChildProcessesDarwin(pid int) bool { return false }