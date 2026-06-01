// Package cmd provides CLI commands.
package cmd

import (
	"fmt"
	"os"

	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/daemon"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: i18n.T("cmd.daemon.short"),
	Long:  `Manage the axons daemon process that runs in the background.`,
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: i18n.T("cmd.daemon.start.short"),
	Long:  `Start the axons daemon process in the background.`,
	RunE:  runStart,
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: i18n.T("cmd.daemon.stop.short"),
	Long:  `Stop the running axons daemon process.`,
	RunE:  runStop,
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: i18n.T("cmd.daemon.ps.short"),
	Long:  `Show the status of the axons daemon.`,
	RunE:  runPS,
}

var (
	daemonFork  bool
	daemonTCP   string
	daemonDebug bool
	daemonLog   string
)

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(startCmd)
	daemonCmd.AddCommand(stopCmd)
	daemonCmd.AddCommand(psCmd)

	startCmd.Flags().BoolVar(&daemonFork, "fork", false, "Run as forked daemon process (internal use)")
	startCmd.Flags().StringVar(&daemonTCP, "tcp", "", "TCP address to listen on (e.g., :8080) for web UI")
	startCmd.Flags().BoolVarP(&daemonDebug, "debug", "d", false, "Run in foreground with debug logging (don't fork)")
	startCmd.Flags().StringVar(&daemonLog, "log", "", "Log file path (default: stdout in debug mode, disabled in fork mode)")
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, _ := config.Load("")

	// Initialize logger
	logCfg := logger.Config{
		Debug:  daemonDebug,
		Output: daemonLog,
	}
	if err := logger.Initialize(logCfg); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Sync()

	// Check if daemon is already running
	if isDaemonRunning(cfg) {
		return fmt.Errorf("daemon is already running")
	}

	// In debug mode, run in foreground
	if daemonDebug {
		logger.Info("Starting daemon in debug mode (foreground)",
			zap.String("socket", cfg.SocketPath()),
			zap.String("tcp", daemonTCP),
		)
		return runForkedDaemon(cfg)
	}

	if daemonFork {
		// This is the forked daemon process
		return runForkedDaemon(cfg)
	}

	// Fork and start the daemon
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	return forkDaemon(cfg, execPath, daemonTCP)
}

// runForkedDaemon runs the forked daemon process
func runForkedDaemon(cfg *config.Config) error {
	d, err := daemon.New(cfg)
	if err != nil {
		logger.Error("Failed to create daemon", zap.Error(err))
		return fmt.Errorf("create daemon: %w", err)
	}
	if daemonTCP != "" {
		d.SetTCPAddr(daemonTCP)
	}

	logger.Info("Daemon starting",
		zap.String("socket", cfg.SocketPath()),
		zap.String("tcp", daemonTCP),
		zap.Bool("debug", daemonDebug),
	)

	if err := d.Run(); err != nil {
		logger.Error("Daemon exited with error", zap.Error(err))
		return err
	}

	logger.Info("Daemon stopped")
	return nil
}

func runStop(cmd *cobra.Command, args []string) error {
	cfg, _ := config.Load("")

	if !isDaemonRunning(cfg) {
		fmt.Println("Daemon is not running")
		return nil
	}

	if err := stopDaemon(cfg); err != nil {
		return fmt.Errorf("stop daemon: %w", err)
	}

	fmt.Println("Daemon stopped")
	return nil
}

func runPS(cmd *cobra.Command, args []string) error {
	cfg, _ := config.Load("")

	if !isDaemonRunning(cfg) {
		fmt.Println("Daemon is not running")
		return nil
	}

	// Use client to get status
	status, err := getDaemonStatus(cfg)
	if err != nil {
		return fmt.Errorf("get daemon status: %w", err)
	}

	fmt.Printf("Status:     %s\n", status.Status)
	fmt.Printf("Version:    %s\n", status.Version)
	fmt.Printf("Uptime:     %s\n", status.Uptime)
	fmt.Printf("Tasks:      %d\n", status.TaskCount)

	if len(status.Tasks) > 0 {
		fmt.Println("\nActive Tasks:")
		for _, task := range status.Tasks {
			fmt.Printf("  %s: %s (%d%%)\n", task.ID, task.Status, task.Progress)
		}
	}

	return nil
}