package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/registry"
)

// CloneService handles remote repository cloning operations.
type CloneService struct {
	config *config.Config
}

// CloneResult represents the result of a clone operation.
type CloneResult struct {
	LocalPath string // Local path where repository was cloned
	Name      string // Repository name
	Provider  string // Provider name (e.g., "github")
	Managed   bool   // Whether the repository is managed by axons
	Branch    string // Branch that was cloned
}

// NewCloneService creates a new clone service instance.
func NewCloneService(cfg *config.Config) *CloneService {
	return &CloneService{
		config: cfg,
	}
}

// Clone clones a remote repository to local filesystem.
// If the repository already exists at the target location, it will be updated (pull).
func (s *CloneService) Clone(ctx context.Context, url, branch, cloneMode, workspace string) (*CloneResult, error) {
	// 1. Detect provider
	provider, err := registry.DetectProvider(url)
	if err != nil {
		return nil, fmt.Errorf("unsupported URL: %w", err)
	}

	// 2. Parse URL to get repository info
	repoInfo, err := provider.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	// 3. Determine clone directory
	var cloneDir string
	if cloneMode == "custom" && workspace != "" {
		// Custom location
		cloneDir = filepath.Join(workspace, repoInfo.Owner, repoInfo.Repo)
	} else {
		// Managed location: ~/.axons/repos/{provider}/{owner}/{repo}
		cloneDir = filepath.Join(
			s.config.Daemon.ClonesDir,
			repoInfo.Provider,
			repoInfo.Owner,
			repoInfo.Repo,
		)
	}

	// 4. Check if directory already exists
	if _, err := os.Stat(cloneDir); err == nil {
		// Directory exists, try to pull latest changes
		if err := provider.Pull(ctx, cloneDir); err != nil {
			// Pull failed, but directory exists - might be a different repo or corrupted
			// We'll proceed with the existing directory
			fmt.Printf("Warning: failed to pull updates: %v\n", err)
		}
		return &CloneResult{
			LocalPath: cloneDir,
			Name:      repoInfo.Repo,
			Provider:  repoInfo.Provider,
			Managed:   cloneMode == "managed",
			Branch:    branch,
		}, nil
	}

	// 5. Create parent directories
	if err := os.MkdirAll(filepath.Dir(cloneDir), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// 6. Set default branch if not specified
	if branch == "" {
		branch = "main"
	}

	// 7. Execute clone
	cloneOpts := &registry.CloneOptions{
		URL:       repoInfo.HTTPSURL,
		LocalPath: cloneDir,
		Branch:    branch,
		Depth:     1, // Shallow clone for faster cloning
		Timeout:   5 * time.Minute,
	}

	if err := provider.Clone(ctx, cloneOpts); err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	return &CloneResult{
		LocalPath: cloneDir,
		Name:      repoInfo.Repo,
		Provider:  repoInfo.Provider,
		Managed:   cloneMode == "managed",
		Branch:    branch,
	}, nil
}