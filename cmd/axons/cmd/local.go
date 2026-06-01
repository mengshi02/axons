// Package cmd provides CLI commands.
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/mengshi02/axons/internal/config"
	"github.com/mengshi02/axons/internal/db"
	"github.com/mengshi02/axons/internal/db/repository"
)

// openManager opens the main DB via db.Manager and returns the manager + close func.
// MigrateMain is handled inside NewManager.
func openManager() (*db.Manager, func(), error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, nil, fmt.Errorf("ensure dirs: %w", err)
	}
	mgr, err := db.NewManager(cfg.Database.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	return mgr, func() { mgr.Close() }, nil
}

// openGlobalRepo opens the main DB and returns a GlobalRepository + close func.
// Use this for commands that only need projects/settings (no nodes/edges).
func openGlobalRepo() (*repository.GlobalRepository, func(), error) {
	mgr, closeFunc, err := openManager()
	if err != nil {
		return nil, nil, err
	}
	return repository.NewGlobal(mgr.MainDB()), closeFunc, nil
}

// openProjectRepo opens the project DB matching the given directory (or cwd).
// It looks up the project in the main DB by path prefix, then opens the project-specific DB.
// Returns the project repository + close func.
func openProjectRepo(dirHint string) (*repository.Repository, func(), error) {
	mgr, closeMgr, err := openManager()
	if err != nil {
		return nil, nil, err
	}

	absDir, err := filepath.Abs(dirHint)
	if err != nil {
		closeMgr()
		return nil, nil, fmt.Errorf("resolve dir: %w", err)
	}

	globalRepo := repository.NewGlobal(mgr.MainDB())
	projects, err := globalRepo.ListProjects()
	if err != nil {
		closeMgr()
		return nil, nil, fmt.Errorf("list projects: %w", err)
	}

	// Find a project whose root_dir matches or is a prefix of absDir
	var projectID string
	for _, p := range projects {
		projAbs, _ := filepath.Abs(p.RootPath)
		if projAbs == absDir || isSubDir(absDir, projAbs) {
			projectID = p.ID
			break
		}
	}
	if projectID == "" {
		closeMgr()
		return nil, nil, fmt.Errorf("no project found for directory %q (run 'axons build' from the project root or create the project first)", absDir)
	}

	pdb, err := mgr.ProjectDB(projectID)
	if err != nil {
		closeMgr()
		return nil, nil, fmt.Errorf("open project db: %w", err)
	}
	repo := repository.New(pdb)
	return repo, closeMgr, nil
}

// openLocalRepo is a backward-compatible helper that opens the project repo
// for the current working directory. CLI commands call this instead of getClient().
func openLocalRepo() (*repository.Repository, func(), error) {
	return openProjectRepo(".")
}

// isSubDir reports whether child is under parent.
func isSubDir(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// rel must not start with ".." to be a sub-directory
	return len(rel) > 0 && rel[0] != '.'
}

// dbPathFromConfig returns the configured DB path.
func dbPathFromConfig() (string, error) {
	cfg, err := config.Load("")
	if err != nil {
		return "", err
	}
	return cfg.Database.Path, nil
}