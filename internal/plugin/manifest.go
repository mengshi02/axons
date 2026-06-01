// Package plugin provides the plugin system for axons.
// It manages plugin lifecycle, registration, and communication.
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// PluginManifest represents the manifest.json of a plugin.
type PluginManifest struct {
	// Basic info
	ID             string `json:"id"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	Description    string `json:"description"`
	Author         string `json:"author"`
	Icon           string `json:"icon"`
	Category       string `json:"category"`
	MinAxonsVersion string `json:"minAxonsVersion"`

	// Permissions
	Permissions []string `json:"permissions"`

	// Backend process (optional, null for pure-frontend plugins)
	Backend *BackendDef `json:"backend"`

	// Frontend UI (optional, null for pure-backend plugins)
	Frontend *FrontendDef `json:"frontend"`

	// Activation events (lazy loading)
	ActivationEvents []string `json:"activationEvents"`

	// Dir is set at runtime, not from JSON — the plugin's filesystem directory.
	Dir string `json:"-"`
}

// BackendDef defines the backend process configuration.
type BackendDef struct {
	Command       []string                       `json:"command"`
	Port          int                            `json:"port"`
	HealthCheck   string                         `json:"healthCheck"`
	ReadyTimeout  string                         `json:"readyTimeout"`
	Env           map[string]string              `json:"env"`
	Install       *InstallDef                    `json:"install"`
	Uninstall     *UninstallDef                  `json:"uninstall"`
	Platforms     map[string]*PlatformOverride    `json:"platforms,omitempty"`
}

// PlatformOverride defines platform-specific overrides for backend configuration.
// When the current OS matches a key in Platforms, the fields present in the
// override are deep-merged into the base BackendDef. Only command/install/uninstall/env
// are overridable; other fields (port, healthCheck, readyTimeout) are never overridden.
type PlatformOverride struct {
	Command   []string          `json:"command,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Install   *InstallDef       `json:"install,omitempty"`
	Uninstall *UninstallDef     `json:"uninstall,omitempty"`
}

// InstallDef defines the install script configuration.
type InstallDef struct {
	Command  []string `json:"command"`
	Timeout  string   `json:"timeout"`
}

// UninstallDef defines the uninstall script configuration.
type UninstallDef struct {
	Command []string       `json:"command"`
	Args    []UninstallArg `json:"args,omitempty"`
}

// UninstallArg describes a parameter accepted by the uninstall script.
type UninstallArg struct {
	Name        string `json:"name"`                  // Parameter name, e.g. "purge_data"
	Type        string `json:"type"`                  // Type: must be "boolean"
	Default     bool   `json:"default,omitempty"`     // Default value
	Description string `json:"description,omitempty"` // Description shown in UI checkbox
}

// LocaleDef defines the locale configuration for a localization plugin.
type LocaleDef struct {
	Language         string            `json:"language"`         // BCP 47 tag, e.g. "zh-CN"
	DisplayName      *LocaleDisplayName `json:"displayName"`     // Native + English names
	Resources        []string          `json:"resources"`       // Frontend resource paths relative to plugin root
	BackendResources []string          `json:"backendResources"` // Backend resource paths relative to plugin root
	PluginTitles     string           `json:"pluginTitles"`    // Path to plugin titles JSON
}

// LocaleDisplayName holds the native and English display names for a locale.
type LocaleDisplayName struct {
	Native  string `json:"native"`  // e.g. "简体中文"
	English string `json:"english"` // e.g. "Chinese (Simplified)"
}

// FrontendDef defines the frontend UI configuration.
type FrontendDef struct {
	Entry    string      `json:"entry"`
	Panels   []PanelDef  `json:"panels"`
	Commands []CommandDef `json:"commands"`
	Skills   []string    `json:"skills"`
	Locale   *LocaleDef  `json:"locale,omitempty"` // Localization plugin locale config
}

// PanelDef defines a plugin panel.
type PanelDef struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	TitleI18n   map[string]string `json:"titleI18n,omitempty"` // Per-locale title overrides, e.g. {"zh-CN": "代码健康"}
	Icon        string            `json:"icon"`
	Location    string            `json:"location"`
	Activator   string            `json:"activator"`
	FooterSlot  string            `json:"footerSlot"`
	Order       int               `json:"order,omitempty"` // Sort weight: lower = earlier; built-in 0-9, plugins 10+
}

// CommandDef defines a plugin command.
type CommandDef struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	TitleI18n map[string]string `json:"titleI18n,omitempty"` // Per-locale title overrides
	Shortcut  string            `json:"shortcut"`
}

// validPluginID matches reverse-domain format: com.example.plugin-name
var validPluginID = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+){1,}$`)

// validSemVer matches semantic version: 1.0.0, 0.1.0-alpha, etc.
var validSemVer = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$`)

// ValidCategories is the set of valid plugin categories.
var ValidCategories = map[string]bool{
	"analysis":      true,
	"visualization": true,
	"search":        true,
	"productivity":  true,
	"localization":  true,
}

// ValidPermissions is the set of valid plugin permissions.
var ValidPermissions = map[string]bool{
	"graph:read":         true,
	"project:read":       true,
	"model:register":     true,
	"panel:create":       true,
	"state:read":         true,
	"state:write":        true,
	"notification:send":  true,
}

// ValidPlatformKeys is the set of valid platform keys in backend.platforms.
var ValidPlatformKeys = map[string]bool{
	"windows": true,
	"linux":   true,
	"darwin":  true,
}

// resolvePlatforms applies platform-specific overrides from backend.platforms
// to the base BackendDef for the current OS (runtime.GOOS).
// After merging, the Platforms field is cleared (nil) so that downstream
// consumers never see it — they only work with the resolved BackendDef.
// If Platforms is nil or no override exists for the current OS, this is a no-op.
func resolvePlatforms(m *PluginManifest) {
	if m.Backend == nil || len(m.Backend.Platforms) == 0 {
		return
	}

	override, ok := m.Backend.Platforms[runtime.GOOS]
	if !ok {
		// No override for current platform — clear and return
		m.Backend.Platforms = nil
		return
	}

	// Deep merge: override non-zero fields into base BackendDef
	if len(override.Command) > 0 {
		m.Backend.Command = override.Command
	}
	if len(override.Env) > 0 {
		if m.Backend.Env == nil {
			m.Backend.Env = make(map[string]string, len(override.Env))
		}
		for k, v := range override.Env {
			m.Backend.Env[k] = v
		}
	}
	if override.Install != nil {
		m.Backend.Install = override.Install
	}
	if override.Uninstall != nil {
		m.Backend.Uninstall = override.Uninstall
	}

	// Clear platforms — resolved, no longer needed
	m.Backend.Platforms = nil
}

// ValidateManifest validates a PluginManifest and returns an error if invalid.
func ValidateManifest(m *PluginManifest) error {
	// Required fields
	if m.ID == "" {
		return fmt.Errorf("manifest: id is required")
	}
	if !validPluginID.MatchString(m.ID) {
		return fmt.Errorf("manifest: id must be in reverse-domain format (e.g. com.axons.plugin), got %q", m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("manifest: name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest: version is required")
	}
	if !validSemVer.MatchString(m.Version) {
		return fmt.Errorf("manifest: version must be semver (e.g. 1.0.0), got %q", m.Version)
	}

	// At least one of backend or frontend must be non-nil
	// Exception: localization plugins may have frontend with no entry/panels
	if m.Backend == nil && m.Frontend == nil {
		return fmt.Errorf("manifest: at least one of backend or frontend must be specified")
	}

	// Category validation
	if m.Category != "" && !ValidCategories[m.Category] {
		return fmt.Errorf("manifest: invalid category %q, must be one of: analysis, visualization, search, productivity, localization", m.Category)
	}

	// Localization-specific validation
	if m.Category == "localization" {
		if m.Frontend == nil || m.Frontend.Locale == nil {
			return fmt.Errorf("manifest: localization plugin must declare frontend.locale")
		}
		if m.Frontend.Locale.Language == "" {
			return fmt.Errorf("manifest: frontend.locale.language is required")
		}
		if len(m.Frontend.Locale.Resources) == 0 {
			return fmt.Errorf("manifest: frontend.locale.resources must have at least one file")
		}
		if m.Backend != nil {
			return fmt.Errorf("manifest: localization plugin must not have backend (must be null)")
		}
		if m.Frontend.Entry != "" {
			return fmt.Errorf("manifest: localization plugin must not have frontend.entry")
		}
		if len(m.Frontend.Panels) > 0 {
			return fmt.Errorf("manifest: localization plugin must not declare frontend.panels")
		}
	}

	// Permissions validation
	for _, perm := range m.Permissions {
		if !ValidPermissions[perm] {
			return fmt.Errorf("manifest: invalid permission %q", perm)
		}
	}

	// Backend validation
	if m.Backend != nil {
		if len(m.Backend.Command) == 0 {
			return fmt.Errorf("manifest: backend.command is required when backend is specified")
		}
		if m.Backend.HealthCheck == "" {
			return fmt.Errorf("manifest: backend.healthCheck is required when backend is specified")
		}
		if m.Backend.ReadyTimeout == "" {
			m.Backend.ReadyTimeout = "10s"
		}
		if m.Backend.Install != nil && len(m.Backend.Install.Command) == 0 {
			return fmt.Errorf("manifest: backend.install.command is required when install is specified")
		}
		if m.Backend.Install != nil && m.Backend.Install.Timeout == "" {
			m.Backend.Install.Timeout = "180s"
		}

		// Platforms validation
		for platformKey, override := range m.Backend.Platforms {
			if !ValidPlatformKeys[platformKey] {
				return fmt.Errorf("manifest: backend.platforms has invalid key %q, must be one of: windows, linux, darwin", platformKey)
			}
			if override != nil && len(override.Command) == 0 && override.Env == nil && override.Install == nil && override.Uninstall == nil {
				return fmt.Errorf("manifest: backend.platforms.%s must specify at least one override field (command, env, install, or uninstall)", platformKey)
			}
			if override != nil && override.Install != nil && len(override.Install.Command) == 0 {
				return fmt.Errorf("manifest: backend.platforms.%s.install.command is required when install is specified", platformKey)
			}
			if override != nil && override.Uninstall != nil && len(override.Uninstall.Command) == 0 {
				return fmt.Errorf("manifest: backend.platforms.%s.uninstall.command is required when uninstall is specified", platformKey)
			}
		}

		// Uninstall args validation
		if m.Backend.Uninstall != nil && len(m.Backend.Uninstall.Args) > 0 {
			seen := make(map[string]bool)
			for _, arg := range m.Backend.Uninstall.Args {
				if arg.Name == "" {
					return fmt.Errorf("manifest: backend.uninstall.args[].name is required")
				}
				if arg.Type != "boolean" {
					return fmt.Errorf("manifest: backend.uninstall.args[].type must be \"boolean\", got %q", arg.Type)
				}
				if seen[arg.Name] {
					return fmt.Errorf("manifest: backend.uninstall.args[].name %q is duplicated", arg.Name)
				}
				seen[arg.Name] = true
			}
		}
	} // end Backend validation

	// Frontend validation
	if m.Frontend != nil {
		// localization plugins skip entry requirement
		if m.Category != "localization" && m.Frontend.Entry == "" {
			return fmt.Errorf("manifest: frontend.entry is required when frontend is specified")
		}
		for i, panel := range m.Frontend.Panels {
			if panel.ID == "" {
				return fmt.Errorf("manifest: frontend.panels[%d].id is required", i)
			}
			if panel.Title == "" {
				return fmt.Errorf("manifest: frontend.panels[%d].title is required", i)
			}
			if panel.Location == "" {
				return fmt.Errorf("manifest: frontend.panels[%d].location is required", i)
			}
			if panel.Activator == "" {
				return fmt.Errorf("manifest: frontend.panels[%d].activator is required", i)
			}
		}
		for i, cmd := range m.Frontend.Commands {
			if cmd.ID == "" {
				return fmt.Errorf("manifest: frontend.commands[%d].id is required", i)
			}
			if cmd.Title == "" {
				return fmt.Errorf("manifest: frontend.commands[%d].title is required", i)
			}
		}
	}

	return nil
}

// LoadManifest reads and validates a manifest.json from the given directory.
func LoadManifest(dir string) (*PluginManifest, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Set runtime directory
	manifest.Dir = dir

	// Resolve relative icon path
	if manifest.Icon != "" && !strings.HasPrefix(manifest.Icon, "/") {
		manifest.Icon = filepath.Join(dir, manifest.Icon)
	}

	// Resolve relative frontend paths
	if manifest.Frontend != nil {
		if manifest.Frontend.Entry != "" && !strings.HasPrefix(manifest.Frontend.Entry, "/") {
			manifest.Frontend.Entry = filepath.Join(dir, manifest.Frontend.Entry)
		}
		for i := range manifest.Frontend.Panels {
			if manifest.Frontend.Panels[i].Icon != "" && !strings.HasPrefix(manifest.Frontend.Panels[i].Icon, "/") {
				manifest.Frontend.Panels[i].Icon = filepath.Join(dir, manifest.Frontend.Panels[i].Icon)
			}
		}
		for i := range manifest.Frontend.Skills {
			if !strings.HasPrefix(manifest.Frontend.Skills[i], "/") {
				manifest.Frontend.Skills[i] = filepath.Join(dir, manifest.Frontend.Skills[i])
			}
		}
	}

	// Resolve platform-specific overrides before validation,
	// so that ValidateManifest checks the resolved (final) values.
	resolvePlatforms(&manifest)

	if err := ValidateManifest(&manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// HasBackend returns true if the plugin has a backend process.
func (m *PluginManifest) HasBackend() bool {
	return m.Backend != nil
}

// HasFrontend returns true if the plugin has frontend UI.
func (m *PluginManifest) HasFrontend() bool {
	return m.Frontend != nil
}

// HasPermission checks if the plugin declares a specific permission.
func (m *PluginManifest) HasPermission(perm string) bool {
	for _, p := range m.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}