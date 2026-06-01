package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var ownersCmd = &cobra.Command{
	Use:   "owners [target]",
	Short: i18n.T("cmd.owners.short"),
	Long: `Analyze code ownership based on CODEOWNERS file (no daemon required).

Examples:
  axons owners                    # Show ownership summary
  axons owners --owner @team-api  # Show files owned by @team-api
  axons owners --boundary         # Show cross-owner call boundaries`,
	Args: cobra.MaximumNArgs(1),
	RunE: runOwners,
}

var (
	ownersOwner    string
	ownersBoundary bool
	ownersFile     []string
	ownersNoTests  bool
	ownersJSON     bool
)

func init() {
	rootCmd.AddCommand(ownersCmd)
	ownersCmd.Flags().StringVar(&ownersOwner, "owner", "", "Filter to a specific owner")
	ownersCmd.Flags().BoolVar(&ownersBoundary, "boundary", false, "Show cross-owner boundary edges")
	ownersCmd.Flags().StringArrayVarP(&ownersFile, "file", "f", nil, "Scope to file (partial match, repeatable)")
	ownersCmd.Flags().BoolVarP(&ownersNoTests, "no-tests", "T", false, "Exclude test/spec files")
	ownersCmd.Flags().BoolVarP(&ownersJSON, "json", "j", false, "Output as JSON")
}

func runOwners(cmd *cobra.Command, args []string) error {
	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	rootDir, _ := filepath.Abs(".")
	svc := core.NewOwnersService(repo)
	result, err := svc.Analyze(&core.OwnersOptions{
		RootDir:    rootDir,
		Owner:      ownersOwner,
		Boundary:   ownersBoundary,
		FileFilter: ownersFile,
		NoTests:    ownersNoTests,
	})
	if err != nil {
		return fmt.Errorf("owners analysis failed: %w", err)
	}

	if ownersJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if result.CodeownersFile != "" {
		fmt.Printf("\nCODEOWNERS: %s\n\n", result.CodeownersFile)
	} else {
		fmt.Println("\nNo CODEOWNERS file found.")
	}
	fmt.Printf("  Total files: %d\n", result.Summary.TotalFiles)
	fmt.Printf("  Unowned: %d\n", result.Summary.UnownedFiles)
	fmt.Printf("  Owners: %d\n\n", len(result.Summary.ByOwner))

	for owner, count := range result.Summary.ByOwner {
		fmt.Printf("    %s  %d files\n", owner, count)
	}

	if ownersOwner != "" && len(result.Files) > 0 {
		fmt.Printf("\n  Files owned by %s (%d):\n", ownersOwner, len(result.Files))
		for _, fo := range result.Files {
			fmt.Printf("    %s\n", fo.File)
		}
	}

	if len(result.Boundaries) > 0 {
		fmt.Printf("\n  Cross-owner boundaries: %d\n\n", len(result.Boundaries))
		limit := 30
		for i, b := range result.Boundaries {
			if i >= limit {
				fmt.Printf("  ... and %d more\n", len(result.Boundaries)-limit)
				break
			}
			srcOwner := strings.Join(b.From.Owners, ", ")
			if srcOwner == "" {
				srcOwner = "(unowned)"
			}
			tgtOwner := strings.Join(b.To.Owners, ", ")
			if tgtOwner == "" {
				tgtOwner = "(unowned)"
			}
			fmt.Printf("    %s [%s] -> %s [%s]\n", b.From.File, srcOwner, b.To.File, tgtOwner)
		}
	}

	if len(result.Boundaries) == 0 && !ownersBoundary && result.CodeownersFile != "" {
		fmt.Fprintln(os.Stderr, "\nTip: use --boundary to show cross-owner call edges")
	}
	return nil
}