//go:build windows

package terminal

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PROCESSENTRY32 is the structure returned by Process32First/Process32Next
// from CreateToolhelp32Snapshot. Aligns with Windows TLHELP32 structure.
// Aligns with VS Code childProcessMonitor.ts Windows path.
type PROCESSENTRY32 struct {
	Size              uint32
	Usage             uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	Threads           uint32
	ParentProcessID   uint32
	PriorityClassBase int32
	Flags             uint32
	ExeFile           [260]uint16 // MAX_PATH
}

var (
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procCreateToolhelp32Snapshot = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32First           = modkernel32.NewProc("Process32FirstW")
	procProcess32Next            = modkernel32.NewProc("Process32NextW")
	procCloseHandle              = modkernel32.NewProc("CloseHandle")
)

const (
	TH32CS_SNAPPROCESS = 0x00000002
	INVALID_HANDLE     = ^uintptr(0) // -1
)

// createToolhelp32Snapshot wraps CreateToolhelp32Snapshot.
func createToolhelp32Snapshot(flags uint32, processID uint32) (windows.Handle, error) {
	ret, _, err := procCreateToolhelp32Snapshot.Call(
		uintptr(flags),
		uintptr(processID),
	)
	if ret == INVALID_HANDLE {
		return 0, err
	}
	return windows.Handle(ret), nil
}

// process32First wraps Process32FirstW.
func process32First(handle windows.Handle, entry *PROCESSENTRY32) error {
	ret, _, err := procProcess32First.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(entry)),
	)
	if ret == 0 {
		return err
	}
	return nil
}

// process32Next wraps Process32NextW.
func process32Next(handle windows.Handle, entry *PROCESSENTRY32) error {
	ret, _, err := procProcess32Next.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(entry)),
	)
	if ret == 0 {
		return err
	}
	return nil
}

// hasChildProcessesWindows uses CreateToolhelp32Snapshot to enumerate
// child processes on Windows. Returns true if the given PID has any
// direct child processes.
//
// Aligns with VS Code childProcessMonitor.ts which uses
// CreateToolhelp32Snapshot on Windows to enumerate the process tree.
//
// This replaces the placeholder in process_monitor.go. On Windows,
// this file is compiled; on non-Windows, the placeholder remains
// (never called due to runtime.GOOS switch).
func hasChildProcessesWindows(pid int) bool {
	snapshot, err := createToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer procCloseHandle.Call(uintptr(snapshot))

	var entry PROCESSENTRY32
	entry.Size = uint32(unsafe.Sizeof(entry))

	// Start enumeration
	if err := process32First(snapshot, &entry); err != nil {
		return false
	}

	// Walk the process list, looking for entries whose ParentProcessID matches
	for {
		if int(entry.ParentProcessID) == pid {
			// Found a child process — but skip the PID 0 (System Idle Process)
			// which can appear as a child of PID 0 due to snapshot noise.
			if entry.ProcessID != 0 {
				return true
			}
		}

		if err := process32Next(snapshot, &entry); err != nil {
			// ERROR_NO_MORE_FILES (0x12) is expected at end of enumeration.
			// Any other error is unexpected but we treat it as "no more entries".
			if errno, ok := err.(syscall.Errno); ok && errno == 0x12 {
				return false
			}
			// For other errors, continue trying or return false
			return false
		}
	}
}