package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var sequenceCmd = &cobra.Command{
	Use:   "sequence <name>",
	Short: i18n.T("cmd.sequence.short"),
	Long: `Generate a Mermaid sequence diagram from call graph edges (no daemon required).

Participants are files (not individual functions). Calls within the same file
become self-messages, keeping diagrams readable.

Examples:
  axons sequence myFunction
  axons sequence myFunction --depth 5`,
	Args: cobra.ExactArgs(1),
	RunE: runSequence,
}

var (
	sequenceDepth   int
	sequenceFile    []string
	sequenceKind    string
	sequenceNoTests bool
)

func init() {
	rootCmd.AddCommand(sequenceCmd)
	sequenceCmd.Flags().IntVarP(&sequenceDepth, "depth", "d", 10, "Max forward traversal depth")
	sequenceCmd.Flags().StringArrayVarP(&sequenceFile, "file", "f", nil, "Scope to file (partial match, repeatable)")
	sequenceCmd.Flags().StringVarP(&sequenceKind, "kind", "k", "", "Filter by symbol kind")
	sequenceCmd.Flags().BoolVarP(&sequenceNoTests, "no-tests", "T", false, "Exclude test/spec files")
}

func runSequence(cmd *cobra.Command, args []string) error {
	name := args[0]

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewSequenceService(repo)
	result, err := svc.Generate(context.Background(), &core.SequenceOptions{
		Name:        name,
		Depth:       sequenceDepth,
		FileFilters: sequenceFile,
		NoTests:     sequenceNoTests,
	})
	if err != nil {
		return err
	}

	if result.Entry == nil {
		fmt.Fprintf(os.Stderr, "Symbol %q not found\n", name)
		return nil
	}

	fmt.Printf("\nSequence from: [%s] %s  %s:%d\n",
		kindIcon(string(result.Entry.Kind)), result.Entry.Name, result.Entry.File, result.Entry.Line)
	fmt.Printf("Participants: %d  Messages: %d\n", len(result.Participants), len(result.Messages))
	if result.Truncated {
		fmt.Printf("  (truncated at depth %d)\n", sequenceDepth)
	}
	fmt.Println()

	if len(result.Messages) == 0 {
		fmt.Println("  (leaf node - no callees)")
		return nil
	}

	// Build aliases
	aliases := buildAliases(result.Participants)

	fmt.Println("sequenceDiagram")
	for _, p := range result.Participants {
		alias := aliases[p]
		fmt.Printf("  participant %s as %s\n", alias, alias)
	}
	for _, msg := range result.Messages {
		fromAlias := aliases[msg.From]
		toAlias := aliases[msg.To]
		label := msg.Function
		fmt.Printf("  %s->>%s: %s\n", fromAlias, toAlias, label)
	}
	return nil
}

func kindIcon(kind string) string {
	switch kind {
	case "function":
		return "f"
	case "method":
		return "m"
	case "class":
		return "C"
	case "interface":
		return "I"
	default:
		return "?"
	}
}

func buildAliases(files []string) map[string]string {
	aliases := make(map[string]string)
	basenames := make(map[string][]string)
	for _, f := range files {
		base := core.BaseFile(f)
		basenames[base] = append(basenames[base], f)
	}
	for base, paths := range basenames {
		if len(paths) == 1 {
			aliases[paths[0]] = sanitizeAlias(base)
		} else {
			for i, p := range paths {
				aliases[p] = sanitizeAlias(fmt.Sprintf("%s_%d", base, i+1))
			}
		}
	}
	return aliases
}

func sanitizeAlias(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, s)
}