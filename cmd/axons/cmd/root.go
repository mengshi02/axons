// Package cmd provides CLI commands.
package cmd

import (
	"fmt"
	"os"

	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/version"
	"github.com/spf13/cobra"
)

var (
	// Version is the application version.
	Version = version.Version
	// Commit is the git commit hash.
	Commit = version.Commit
	// Date is the build date.
	Date = version.Date
)

var rootCmd = &cobra.Command{
	Use:   "axons",
	Short: i18n.T("cmd.root.short"),
	Long:  i18n.T("cmd.root.long"),
	Version: Version,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
}

// GetRootCmd returns the root command for testing.
func GetRootCmd() *cobra.Command {
	return rootCmd
}

// SetVersion sets the version information.
func SetVersion(version, commit, date string) {
	Version = version
	Commit = commit
	Date = date
	rootCmd.Version = version
}

// AddCommand adds a subcommand to the root command.
func AddCommand(cmd *cobra.Command) {
	rootCmd.AddCommand(cmd)
}

// ExitWithError prints an error message and exits with code 1.
func ExitWithError(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
	os.Exit(1)
}