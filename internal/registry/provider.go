// Package registry provides multi-repository registration and Git provider management.
package registry

import (
	"context"
	"time"
)

// GitProvider defines the interface for Git hosting platform providers.
// Each provider (GitHub, GitLab, etc.) implements this interface to handle
// repository cloning, pulling, and URL parsing.
type GitProvider interface {
	// Information
	Name() string                      // "github", "gitlab", etc.
	CanHandle(url string) bool         // Check if can handle this URL
	ParseURL(url string) (*RepoInfo, error) // Extract info from URL

	// Operations
	Clone(ctx context.Context, opts *CloneOptions) error
	Pull(ctx context.Context, localPath string) error

	// Authentication (optional)
	SetCredentials(creds *Credentials)
}

// RepoInfo represents parsed repository information from a Git URL.
type RepoInfo struct {
	Provider  string // "github", "gitlab", "bitbucket"
	Owner     string // Repository owner/organization
	Repo      string // Repository name
	HTTPSURL  string // HTTPS clone URL
	SSHURL    string // SSH clone URL
	IsPrivate bool   // Whether the repository is private
}

// CloneOptions represents configuration for cloning a repository.
type CloneOptions struct {
	URL        string        // Repository URL to clone
	LocalPath  string        // Local directory to clone into
	Branch     string        // Branch to checkout (optional)
	Depth      int           // Shallow clone depth (0 = full clone)
	Timeout    time.Duration // Clone timeout
}

// Credentials represents authentication information for Git operations.
type Credentials struct {
	Type     string // "token", "ssh_key", "password"
	Token    string // Personal access token
	SSHKey   string // SSH private key path
	Username string // Username for basic auth
	Password string // Password for basic auth
}

// RemoteRepoInfo represents detailed repository information from API.
type RemoteRepoInfo struct {
	Name          string    // Repository name
	FullName      string    // Full name (owner/repo)
	Description   string    // Repository description
	Stars         int       // Star count
	Forks         int       // Fork count
	IsPrivate     bool      // Whether private
	DefaultBranch string    // Default branch name
	UpdatedAt     time.Time // Last update time
}