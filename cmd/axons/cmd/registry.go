package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mengshi02/axons/internal/core"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: i18n.T("cmd.registry.short"),
	Long: `Manage the registry of multiple code repositories (no daemon required).

The registry stores project paths in ~/.axons/registry.json.

Examples:
  axons registry add /path/to/project
  axons registry list
  axons registry remove myproject
  axons registry prune --ttl 30`,
}

var (
	registryListJSON     bool
	registryAddName      string
	registryPruneTTL     int
	registryPruneExclude []string
	registryPruneDryRun  bool
)

func init() {
	AddCommand(registryCmd)

	listCmd := &cobra.Command{
		Use: "list", Short: i18n.T("cmd.registry.list.short"),
		RunE: runRegistryList,
	}
	listCmd.Flags().BoolVar(&registryListJSON, "json", false, "Output in JSON format")
	registryCmd.AddCommand(listCmd)

	addCmd := &cobra.Command{
		Use: "add <path>", Short: "Register a repository",
		Args: cobra.ExactArgs(1), RunE: runRegistryAdd,
	}
	addCmd.Flags().StringVarP(&registryAddName, "name", "n", "", "Custom name for the repository")
	registryCmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use: "remove <name>", Short: "Remove a repository from the registry",
		Aliases: []string{"rm", "delete"},
		Args:    cobra.ExactArgs(1), RunE: runRegistryRemove,
	}
	registryCmd.AddCommand(removeCmd)

	pruneCmd := &cobra.Command{
		Use: "prune", Short: "Remove stale registry entries",
		RunE: runRegistryPrune,
	}
	pruneCmd.Flags().IntVarP(&registryPruneTTL, "ttl", "t", 30, "TTL in days for inactive entries")
	pruneCmd.Flags().StringSliceVar(&registryPruneExclude, "exclude", nil, "Names to exclude from pruning")
	pruneCmd.Flags().BoolVar(&registryPruneDryRun, "dry-run", false, "Show what would be removed")
	registryCmd.AddCommand(pruneCmd)
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	svc := core.NewRegistryService()
	repos := svc.List()
	if len(repos) == 0 {
		fmt.Println("No repositories registered.")
		fmt.Println("\nUse 'axons registry add <path>' to add a repository.")
		return nil
	}
	if registryListJSON {
		data, _ := json.MarshalIndent(repos, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("%-20s %-40s %s\n", "NAME", "PATH", "LAST ACCESSED")
	fmt.Printf("%-20s %-40s %s\n", "----", "----", "-------------")
	for _, repo := range repos {
		fmt.Printf("%-20s %-40s %s\n", repo.Name, truncatePath(repo.Path, 40), repo.LastAccessedAt.Format("2006-01-02"))
	}
	fmt.Printf("\n%d repositories registered.\n", len(repos))
	return nil
}

func runRegistryAdd(cmd *cobra.Command, args []string) error {
	// First add to the simple registry
	svc := core.NewRegistryService()
	repo, err := svc.Add(args[0], registryAddName)
	if err != nil {
		return fmt.Errorf("register failed: %w", err)
	}

	// Also create project in global database
	mgr, closeMgr, err := openManager()
	if err != nil {
		return fmt.Errorf("open database manager: %w", err)
	}
	defer closeMgr()

	globalRepo := repository.NewGlobal(mgr.MainDB())

	// Check if project already exists
	projects, err := globalRepo.ListProjects()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	// Check by name or path
	var existingID string
	for _, p := range projects {
		if p.Name == repo.Name || p.RootPath == repo.Path {
			existingID = p.ID
			break
		}
	}

	if existingID != "" {
		fmt.Printf("Project already exists in database (ID: %s)\n", existingID)
		fmt.Printf("Registered:\n  Name: %s\n  Path: %s\n  DB:   %s\n", repo.Name, repo.Path, repo.DBPath)
		return nil
	}

	// Create new project in global database
	newProjectID := generateProjectID()
	project, err := globalRepo.CreateProject(newProjectID, repo.Name, repo.Path)
	if err != nil {
		return fmt.Errorf("create project in database: %w", err)
	}

	// Pre-warm the project database
	if _, err := mgr.ProjectDB(project.ID); err != nil {
		fmt.Printf("Warning: failed to initialize project DB: %v\n", err)
	}

	fmt.Printf("Registered and created project:\n")
	fmt.Printf("  Name: %s\n", repo.Name)
	fmt.Printf("  Path: %s\n", repo.Path)
	fmt.Printf("  DB:   %s\n", repo.DBPath)
	fmt.Printf("  ID:   %s\n", project.ID)
	return nil
}

func generateProjectID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func runRegistryRemove(cmd *cobra.Command, args []string) error {
	svc := core.NewRegistryService()
	ok, err := svc.Remove(args[0])
	if err != nil {
		return fmt.Errorf("remove failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("repository %q not found", args[0])
	}
	fmt.Printf("Removed %q from registry.\n", args[0])
	return nil
}

func runRegistryPrune(cmd *cobra.Command, args []string) error {
	svc := core.NewRegistryService()
	pruned, err := svc.Prune(registryPruneTTL, registryPruneExclude, registryPruneDryRun)
	if err != nil {
		return fmt.Errorf("prune failed: %w", err)
	}
	if len(pruned) == 0 {
		fmt.Println("No stale entries found.")
		return nil
	}
	action := "Would remove"
	if !registryPruneDryRun {
		action = "Removed"
	}
	fmt.Printf("%s %d stale entr(y/ies):\n", action, len(pruned))
	for _, p := range pruned {
		fmt.Printf("  %-20s %-40s %s\n", p.Name, truncatePath(p.Path, 40), p.Reason)
	}
	return nil
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

func printJSON(data interface{}) {
	b, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(b))
}