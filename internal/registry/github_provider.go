package registry

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// GitHubProvider implements GitProvider for GitHub repositories.
type GitHubProvider struct {
	creds *Credentials
}

func init() {
	// Auto-register GitHub provider
	RegisterProvider(&GitHubProvider{})
}

// Name returns the provider name.
func (p *GitHubProvider) Name() string {
	return "github"
}

// CanHandle checks if the URL is a GitHub repository URL.
func (p *GitHubProvider) CanHandle(url string) bool {
	return strings.Contains(url, "github.com")
}

// ParseURL parses a GitHub URL and extracts repository information.
// Supports formats:
//   - https://github.com/owner/repo
//   - https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git
func (p *GitHubProvider) ParseURL(url string) (*RepoInfo, error) {
	// Normalize URL
	url = strings.TrimSpace(url)

	// Pattern for HTTPS URLs: https://github.com/owner/repo[.git]
	httpsPattern := regexp.MustCompile(`https://github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:/.*)?$`)

	// Pattern for SSH URLs: git@github.com:owner/repo.git
	sshPattern := regexp.MustCompile(`git@github\.com:([^/]+)/([^/]+?)(?:\.git)?(?:/.*)?$`)

	var owner, repo string

	if matches := httpsPattern.FindStringSubmatch(url); len(matches) >= 3 {
		owner = matches[1]
		repo = matches[2]
	} else if matches := sshPattern.FindStringSubmatch(url); len(matches) >= 3 {
		owner = matches[1]
		repo = matches[2]
	} else {
		return nil, fmt.Errorf("invalid GitHub URL format: %s", url)
	}

	// Remove .git suffix if present
	repo = strings.TrimSuffix(repo, ".git")

	return &RepoInfo{
		Provider:  "github",
		Owner:     owner,
		Repo:      repo,
		HTTPSURL:  fmt.Sprintf("https://github.com/%s/%s.git", owner, repo),
		SSHURL:    fmt.Sprintf("git@github.com:%s/%s.git", owner, repo),
		IsPrivate: false, // Would need API call to determine
	}, nil
}

// Clone clones a GitHub repository to the specified local path.
func (p *GitHubProvider) Clone(ctx context.Context, opts *CloneOptions) error {
	cloneOpts := &git.CloneOptions{
		URL: opts.URL,
	}

	// Add authentication if credentials are set
	if auth := p.getAuth(); auth != nil {
		cloneOpts.Auth = auth
	}

	// Set branch if specified
	if opts.Branch != "" {
		cloneOpts.ReferenceName = plumbing.ReferenceName("refs/heads/" + opts.Branch)
	}

	// Set depth for shallow clone
	if opts.Depth > 0 {
		cloneOpts.Depth = opts.Depth
	}

	// Set timeout context
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Execute clone
	_, err := git.PlainCloneContext(ctx, opts.LocalPath, false, cloneOpts)
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	return nil
}

// Pull pulls the latest changes for a cloned repository.
func (p *GitHubProvider) Pull(ctx context.Context, localPath string) error {
	// Open the repository
	repo, err := git.PlainOpen(localPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Get working tree
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Pull options
	pullOpts := &git.PullOptions{}
	if auth := p.getAuth(); auth != nil {
		pullOpts.Auth = auth
	}

	// Execute pull
	err = wt.PullContext(ctx, pullOpts)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to pull: %w", err)
	}

	return nil
}

// SetCredentials sets authentication credentials for the provider.
func (p *GitHubProvider) SetCredentials(creds *Credentials) {
	p.creds = creds
}

// getAuth returns the appropriate authentication method based on credentials.
func (p *GitHubProvider) getAuth() transport.AuthMethod {
	if p.creds == nil {
		return nil
	}

	switch p.creds.Type {
	case "token":
		// Use token-based authentication
		return &http.BasicAuth{
			Username: "oauth2", // GitHub requires this exact username for token auth
			Password: p.creds.Token,
		}

	case "ssh_key":
		// Use SSH key authentication
		auth, err := ssh.NewPublicKeysFromFile("git", p.creds.SSHKey, "")
		if err != nil {
			return nil
		}
		return auth

	case "password":
		// Use basic authentication
		return &http.BasicAuth{
			Username: p.creds.Username,
			Password: p.creds.Password,
		}

	default:
		return nil
	}
}
