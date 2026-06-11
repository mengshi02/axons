//go:build darwin

package terminal

import (
	"os/exec"
	"strconv"

	"golang.org/x/sys/unix"
)

// hasChildProcessesDarwin uses unix.SysctlKinfoProcSlice to enumerate
// processes on macOS without shelling out to pgrep. Reads the kinfo_proc
// array and scans for entries whose eproc.ppid matches the target PID.
//
// Advantages over pgrep:
//   - Zero fork+exec overhead (~0.1ms vs ~5-10ms)
//   - No PATH dependency (works in sandboxed/container environments)
//   - Atomic snapshot (no race between check and process exit)
//   - No external command security restrictions
//
// Falls back to pgrep if sysctl fails (e.g. insufficient permissions).
// Aligns with VS Code childProcessMonitor.ts macOS path.
func hasChildProcessesDarwin(pid int) bool {
	// Fetch all processes via sysctl kern.proc.all
	// KERN_PROC_ALL = 0 — list all processes
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		// Fallback to pgrep if sysctl fails (e.g. sandbox restriction)
		return hasChildProcessesDarwinPgrep(pid)
	}

	// Scan for entries whose parent PID matches
	for _, proc := range procs {
		if proc.Eproc.Ppid == int32(pid) && proc.Proc.P_pid > 0 {
			return true
		}
	}

	return false
}

// hasChildProcessesDarwinPgrep is the pgrep-based fallback for macOS.
// Used when sysctl fails (e.g. sandbox restrictions, macOS version changes).
// This is the original implementation, kept as a safety net.
func hasChildProcessesDarwinPgrep(pid int) bool {
	cmd := exec.Command("pgrep", "-P", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		// pgrep returns non-zero exit code when no children found
		return false
	}
	// If output is non-empty, there are child processes
	for _, b := range output {
		if b >= '0' && b <= '9' {
			return true
		}
	}
	return false
}