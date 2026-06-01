package core

import (
	"github.com/mengshi02/axons/internal/registry"
)

// RegistryService wraps registry.Registry for CLI use.
type RegistryService struct {
	reg *registry.Registry
}

// NewRegistryService creates a RegistryService using the default registry path.
func NewRegistryService() *RegistryService {
	reg := registry.New(nil)
	_ = reg.Load()
	return &RegistryService{reg: reg}
}

// Add registers a repository path.
func (s *RegistryService) Add(path, name string) (*registry.Repository, error) {
	return s.reg.Register(path, name)
}

// Remove unregisters a repository by name.
func (s *RegistryService) Remove(name string) (bool, error) {
	return s.reg.Unregister(name)
}

// List returns all registered repositories.
func (s *RegistryService) List() []*registry.Repository {
	return s.reg.List()
}

// Prune removes stale entries.
func (s *RegistryService) Prune(ttlDays int, exclude []string, dryRun bool) ([]*registry.PruneResult, error) {
	return s.reg.Prune(ttlDays, exclude, dryRun)
}