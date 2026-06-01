// Package cmd provides CLI commands.
package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build [path]",
	Short: i18n.T("cmd.build.short"),
	Long: `Build a code graph from the specified directory (no daemon required).

If no path is provided, the current directory is used.

Examples:
  axons build                    # Build graph for current directory
  axons build ./src              # Build graph for src directory
  axons build --full ./project   # Force full rebuild`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBuild,
}

var (
	buildFull     bool
	buildExclude  []string
	buildDataflow bool
	buildAST      bool
	buildVerbose  bool
	buildTimeout  time.Duration
)

func init() {
	AddCommand(buildCmd)
	buildCmd.Flags().BoolVarP(&buildFull, "full", "f", false, "Force full rebuild")
	buildCmd.Flags().StringArrayVarP(&buildExclude, "exclude", "e", nil, "Exclude patterns")
	buildCmd.Flags().BoolVar(&buildDataflow, "dataflow", false, "Include dataflow analysis")
	buildCmd.Flags().BoolVar(&buildAST, "ast", false, "Include AST nodes")
	buildCmd.Flags().BoolVarP(&buildVerbose, "verbose", "v", false, "Verbose output")
	buildCmd.Flags().DurationVar(&buildTimeout, "timeout", 10*time.Minute, "Build timeout")
}

func runBuild(cmd *cobra.Command, args []string) error {
	rootDir := "."
	if len(args) > 0 {
		rootDir = args[0]
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewBuildService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	if buildVerbose {
		fmt.Printf("Building graph: %s\n", absRoot)
	}

	result, err := svc.Build(ctx, &core.BuildOptions{
		RootDir:         absRoot,
		FullBuild:       buildFull,
		ExcludePatterns: buildExclude,
		IncludeDataflow: buildDataflow,
		IncludeAST:      buildAST,
	})
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Printf("Build complete in %s\n", result.Duration.Round(time.Millisecond))
	fmt.Printf("  Files parsed:  %d\n", result.FilesParsed)
	fmt.Printf("  Nodes created: %d\n", result.NodesCreated)
	fmt.Printf("  Edges created: %d\n", result.EdgesCreated)
	if len(result.ChangedFiles) > 0 {
		fmt.Printf("  Changed files: %d\n", len(result.ChangedFiles))
	}
	return nil
}