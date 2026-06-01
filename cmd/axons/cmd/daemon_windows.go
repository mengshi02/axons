//go:build windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/daemon"
)

// forkDaemon forks and starts the daemon process (Windows implementation)
// Note: Windows doesn't support fork/Setsid, so we just start the process in background
func forkDaemon(cfg *config.Config, execPath, tcpAddr string) error {
	daemonArgs := []string{"daemon", "start", "--fork"}
	if tcpAddr != "" {
		daemonArgs = append(daemonArgs, "--tcp", tcpAddr)
	}
	daemonCmd := exec.Command(execPath, daemonArgs...)
	daemonCmd.Env = append(os.Environ(), "DAEMON=1")
	daemonCmd.Stdin = nil
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Printf("Daemon started (PID: %d)\n", daemonCmd.Process.Pid)
	return nil
}

// isDaemonRunning checks if the daemon is running
func isDaemonRunning(cfg *config.Config) bool {
	return daemon.IsRunningBySocket(cfg.SocketPath())
}

// stopDaemon stops the running daemon
func stopDaemon(cfg *config.Config) error {
	return daemon.Stop(cfg.SocketPath())
}

// getDaemonStatus gets the daemon status
func getDaemonStatus(cfg *config.Config) (*daemon.StatusResponse, error) {
	return daemon.GetStatus(cfg.SocketPath())
}