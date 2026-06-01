package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: i18n.T("cmd.snapshot.short"),
	Long: `Manage snapshots of the graph database (no daemon required).

Snapshots are stored in .axons/snapshots/ and can be used to:
- Save the current state before risky refactoring
- Compare different code states
- Roll back to a previous analysis state

Examples:
  axons snapshot save v1.0          # Save a snapshot
  axons snapshot list               # List all snapshots
  axons snapshot restore v1.0       # Restore a snapshot
  axons snapshot delete v1.0        # Delete a snapshot`,
}

var (
	snapshotForce bool
	snapshotJSON  bool
)

func init() {
	rootCmd.AddCommand(snapshotCmd)

	// save subcommand
	saveCmd := &cobra.Command{
		Use:   "save <name>",
		Short: i18n.T("cmd.snapshot.save.short"),
		Long: `Save a snapshot of the current graph database.

Copies the SQLite DB file to .axons/snapshots/<name>.db.`,
		Args: cobra.ExactArgs(1),
		Run:  runSnapshotSave,
	}
	saveCmd.Flags().BoolVar(&snapshotForce, "force", false, "Overwrite existing snapshot")
	snapshotCmd.AddCommand(saveCmd)

	// restore subcommand
	restoreCmd := &cobra.Command{
		Use:   "restore <name>",
		Short: "Restore a snapshot over the current graph database",
		Args:  cobra.ExactArgs(1),
		Run:   runSnapshotRestore,
	}
	snapshotCmd.AddCommand(restoreCmd)

	// list subcommand
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all saved snapshots",
		Run:   runSnapshotList,
	}
	listCmd.Flags().BoolVarP(&snapshotJSON, "json", "j", false, "Output as JSON")
	snapshotCmd.AddCommand(listCmd)

	// delete subcommand
	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a saved snapshot",
		Args:  cobra.ExactArgs(1),
		Run:   runSnapshotDelete,
	}
	snapshotCmd.AddCommand(deleteCmd)
}

func newSnapshotSvc() (*core.SnapshotService, error) {
	dbPath, err := dbPathFromConfig()
	if err != nil {
		return nil, err
	}
	return core.NewSnapshotService(dbPath), nil
}

func runSnapshotSave(cmd *cobra.Command, args []string) {
	name := args[0]

	svc, err := newSnapshotSvc()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	info, err := svc.Save(name, snapshotForce)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Snapshot saved: %s (%s)\n", info.Name, formatSize(info.Size))
}

func runSnapshotRestore(cmd *cobra.Command, args []string) {
	name := args[0]

	svc, err := newSnapshotSvc()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := svc.Restore(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Snapshot %q restored.\n", name)
}

func runSnapshotList(cmd *cobra.Command, args []string) {
	svc, err := newSnapshotSvc()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	infos, err := svc.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if snapshotJSON {
		data, _ := json.MarshalIndent(infos, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(infos) == 0 {
		fmt.Println("No snapshots found.")
		return
	}

	fmt.Printf("Snapshots (%d):\n\n", len(infos))
	for _, s := range infos {
		fmt.Printf("  %-30s %10s  %s\n", s.Name, formatSize(s.Size), s.CreatedAt)
	}
}

func runSnapshotDelete(cmd *cobra.Command, args []string) {
	name := args[0]

	svc, err := newSnapshotSvc()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := svc.Delete(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Snapshot %q deleted.\n", name)
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}