// Package cmd provides CLI commands.
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

// watchCmd represents the watch command.
var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: i18n.T("cmd.watch.short"),
	Long: `Manage file watchers that monitor directories for changes.

The watch command runs in the daemon background and records file changes
to a journal file. This enables Tier 0 incremental detection for subsequent
builds, providing the fastest possible rebuild times.

Examples:
  # Start watching the current directory
  axons watch start

  # Start watching a specific directory
  axons watch start /path/to/project

  # Check watch status
  axons watch status

  # List all active watchers
  axons watch list

  # Stop watching
  axons watch stop
  axons watch stop /path/to/project`,
}

var watchStartCmd = &cobra.Command{
	Use:   "start [path]",
	Short: i18n.T("cmd.watch.start.short"),
	Long: `Start watching a directory for file changes.

The watcher runs in the daemon background and records all file changes
to a journal file for incremental builds.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatchStart,
}

var watchStopCmd = &cobra.Command{
	Use:   "stop [path]",
	Short: "Stop watching a directory",
	Long:  `Stop the file watcher for the specified directory.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWatchStop,
}

var watchStatusCmd = &cobra.Command{
	Use:   "status [path]",
	Short: "Show watch status",
	Long:  `Show the status of the file watcher for the specified directory.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWatchStatus,
}

var watchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active watchers",
	Long:  `List all active file watchers running in the daemon.`,
	Args:  cobra.NoArgs,
	RunE:  runWatchList,
}

func init() {
	rootCmd.AddCommand(watchCmd)
	watchCmd.AddCommand(watchStartCmd)
	watchCmd.AddCommand(watchStopCmd)
	watchCmd.AddCommand(watchStatusCmd)
	watchCmd.AddCommand(watchListCmd)
}

func runWatchStart(cmd *cobra.Command, args []string) error {
	// Get root directory
	rootDir := "."
	if len(args) > 0 {
		rootDir = args[0]
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Get daemon client
	c, err := getClient()
	if err != nil {
		return err
	}

	// Start watching via daemon
	resp, err := c.WatchStart(absRoot)
	if err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	switch resp.Status {
	case "started":
		fmt.Printf("Started watching: %s\n", resp.RootDir)
		fmt.Println("File changes will be recorded for incremental builds.")
	case "already_watching":
		fmt.Printf("Already watching: %s\n", resp.RootDir)
		fmt.Printf("Started at: %s\n", resp.StartTime.Format("2006-01-02 15:04:05"))
	default:
		fmt.Printf("Status: %s\n", resp.Status)
	}

	return nil
}

func runWatchStop(cmd *cobra.Command, args []string) error {
	// Get root directory
	rootDir := ""
	if len(args) > 0 {
		absRoot, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}
		rootDir = absRoot
	}

	// Get daemon client
	c, err := getClient()
	if err != nil {
		return err
	}

	// Stop watching via daemon
	resp, err := c.WatchStop(rootDir)
	if err != nil {
		return fmt.Errorf("failed to stop watcher: %w", err)
	}

	fmt.Printf("Stopped watching: %s\n", resp.RootDir)
	return nil
}

func runWatchStatus(cmd *cobra.Command, args []string) error {
	// Get root directory
	rootDir := ""
	if len(args) > 0 {
		absRoot, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}
		rootDir = absRoot
	}

	// Get daemon client
	c, err := getClient()
	if err != nil {
		return err
	}

	// Get watch status
	resp, err := c.WatchStatus(rootDir)
	if err != nil {
		return fmt.Errorf("failed to get watch status: %w", err)
	}

	if resp.RootDir != "" {
		fmt.Printf("Directory: %s\n", resp.RootDir)
		fmt.Printf("Status: %s\n", resp.Status)
		if !resp.StartTime.IsZero() {
			fmt.Printf("Started: %s\n", resp.StartTime.Format("2006-01-02 15:04:05"))
		}
	} else {
		fmt.Printf("Status: %s\n", resp.Status)
	}

	return nil
}

func runWatchList(cmd *cobra.Command, args []string) error {
	// Get daemon client
	c, err := getClient()
	if err != nil {
		return err
	}

	// List all watchers
	resp, err := c.WatchList()
	if err != nil {
		return fmt.Errorf("failed to list watchers: %w", err)
	}

	if resp.Count == 0 {
		fmt.Println("No active watchers.")
		return nil
	}

	fmt.Printf("Active watchers (%d):\n", resp.Count)
	for i, w := range resp.Watchers {
		fmt.Printf("  %d. %s\n", i+1, w.RootDir)
		fmt.Printf("     Status: %s, Started: %s\n", w.Status, w.StartTime.Format("2006-01-02 15:04:05"))
	}

	return nil
}