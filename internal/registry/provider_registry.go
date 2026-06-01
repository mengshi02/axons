package registry

import (
	"fmt"
	"sync"
)

// ProviderRegistry manages all registered Git providers.
type ProviderRegistry struct {
	providers map[string]GitProvider
	mu        sync.RWMutex
}

// globalProviderRegistry is the global instance of provider registry.
var globalProviderRegistry = NewProviderRegistry()

// NewProviderRegistry creates a new provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]GitProvider),
	}
}

// Register registers a Git provider.
func (r *ProviderRegistry) Register(provider GitProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.Name()] = provider
}

// Get retrieves a provider by name.
func (r *ProviderRegistry) Get(name string) (GitProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return provider, nil
}

// Detect automatically detects which provider can handle the given URL.
func (r *ProviderRegistry) Detect(url string) (GitProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	for _, provider := range r.providers {
		if provider.CanHandle(url) {
			return provider, nil
		}
	}
	
	return nil, fmt.Errorf("no provider found for URL: %s", url)
}

// List returns all registered providers.
func (r *ProviderRegistry) List() []GitProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	providers := make([]GitProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	return providers
}

// Global functions for convenience

// RegisterProvider registers a Git provider to the global registry.
func RegisterProvider(provider GitProvider) {
	globalProviderRegistry.Register(provider)
}

// GetProvider retrieves a provider by name from the global registry.
func GetProvider(name string) (GitProvider, error) {
	return globalProviderRegistry.Get(name)
}

// DetectProvider automatically detects which provider can handle the given URL.
func DetectProvider(url string) (GitProvider, error) {
	return globalProviderRegistry.Detect(url)
}

// ListProviders returns all registered providers from the global registry.
func ListProviders() []GitProvider {
	return globalProviderRegistry.List()
}