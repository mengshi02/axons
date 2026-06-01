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

var embedCmd = &cobra.Command{
	Use:   "embed [dir]",
	Short: i18n.T("cmd.embed.short"),
	Long: `Build semantic embeddings for all functions/methods/classes (no daemon required).

Examples:
  axons embed                                # Embed with default settings (ollama)
  axons embed --provider openai              # Embed with OpenAI
  axons embed --strategy full                # Force re-embed all symbols
  axons embed --provider ollama --model nomic-embed-text`,
	Args: cobra.MaximumNArgs(1),
	RunE: runEmbed,
}

var (
	embedTimeout  time.Duration
	embedProvider string
	embedModel    string
	embedStrategy string
	embedBatch    int
	embedBaseURL  string
	embedAPIKey   string
)

func init() {
	AddCommand(embedCmd)
	embedCmd.Flags().DurationVar(&embedTimeout, "timeout", 10*time.Minute, "Embedding timeout")
	embedCmd.Flags().StringVarP(&embedProvider, "provider", "p", "ollama", "Embedding provider: openai, ollama")
	embedCmd.Flags().StringVarP(&embedModel, "model", "m", "", "Embedding model")
	embedCmd.Flags().StringVarP(&embedStrategy, "strategy", "s", "incremental", "Embedding strategy: incremental, full")
	embedCmd.Flags().IntVar(&embedBatch, "batch", 50, "Batch size for embedding API calls")
	embedCmd.Flags().StringVar(&embedBaseURL, "base-url", "", "Custom base URL for embedding API")
	embedCmd.Flags().StringVar(&embedAPIKey, "api-key", "", "API key (or set OPENAI_API_KEY env var)")
}

func runEmbed(cmd *cobra.Command, args []string) error {
	rootDir := "."
	if len(args) > 0 {
		rootDir = args[0]
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	_ = absRoot // rootDir is informational; embedder uses repo directly

	repo, closeDB, err := openLocalRepo()
	if err != nil {
		return fmt.Errorf("open local db: %w", err)
	}
	defer closeDB()

	svc := core.NewEmbedService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), embedTimeout)
	defer cancel()

	opts := &core.EmbedOptions{
		Provider: embedProvider,
		Model:    embedModel,
		BaseURL:  embedBaseURL,
		APIKey:   embedAPIKey,
		Strategy: embedStrategy,
		Batch:    embedBatch,
	}

	fmt.Printf("Building embeddings...\n")
	fmt.Printf("  Provider : %s\n", embedProvider)
	if embedModel != "" {
		fmt.Printf("  Model    : %s\n", embedModel)
	}
	fmt.Printf("  Strategy : %s\n", embedStrategy)
	fmt.Println()

	result, err := svc.Embed(ctx, opts)
	if err != nil {
		return fmt.Errorf("embedding failed: %w", err)
	}

	fmt.Printf("Embedding complete!\n")
	fmt.Printf("  Total symbols : %d\n", result.Total)
	fmt.Printf("  New           : %d\n", result.NewCount)
	fmt.Printf("  Updated       : %d\n", result.UpdatedCount)

	return nil
}