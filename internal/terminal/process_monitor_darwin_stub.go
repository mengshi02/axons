//go:build darwin

package terminal

// hasChildProcessesWindows is a stub on Darwin (never called, needed for compilation).
func hasChildProcessesWindows(pid int) bool { return false }