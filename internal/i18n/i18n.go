// Package i18n provides a lightweight internationalization system for Axons.
//
// It uses TOML language packs with go:embed for the default English bundle,
// and supports runtime loading/unloading of additional locales (e.g. from
// language plugins). The active locale is a global setting suitable for the
// single-user desktop model.
//
// LIMITATION: locale is global (not per-request). This is intentional for
// Axons' single-user desktop application model.
package i18n

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

//go:embed locales/en.toml
var defaultFS embed.FS

var (
	mu      sync.RWMutex
	bundles = map[string]map[string]string{} // locale → key → value
	locale  = "en"
)

func init() {
	// Load embedded English bundle
	loadFromFS("en", defaultFS, "locales/en.toml")
}

// T translates a key with optional interpolation.
// Keys use dot notation: "cmd.build.short", "api.error.pluginNotFound".
// If the key is missing in the current locale, it falls back to English.
// If also missing in English, the key itself is returned as fallback.
func T(key string, args ...any) string {
	mu.RLock()
	defer mu.RUnlock()

	bundle, ok := bundles[locale]
	if !ok {
		bundle = bundles["en"]
	}

	template, ok := bundle[key]
	if !ok {
		// fallback to en
		if bundle = bundles["en"]; bundle != nil {
			template, ok = bundle[key]
		}
		if !ok {
			return key // key itself as fallback
		}
	}

	if len(args) > 0 {
		// Support {{name}} map interpolation
		if m, ok := args[0].(map[string]string); ok {
			result := template
			for k, v := range m {
				result = strings.ReplaceAll(result, "{{"+k+"}}", v)
			}
			return result
		}
		return fmt.Sprintf(template, args...)
	}
	return template
}

// SetLocale changes the active locale.
func SetLocale(l string) {
	mu.Lock()
	defer mu.Unlock()
	locale = l
}

// GetLocale returns the current active locale.
func GetLocale() string {
	mu.RLock()
	defer mu.RUnlock()
	return locale
}

// LoadBundle loads a locale bundle from a directory by scanning .toml files.
// Supports runtime incremental loading (language plugin install, no restart).
func LoadBundle(loc, dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	merged := map[string]string{}
	if existing, ok := bundles[loc]; ok {
		// Preserve existing keys if merging
		for k, v := range existing {
			merged[k] = v
		}
	}

	for _, f := range files {
		name := f.Name()
		// Skip macOS AppleDouble resource fork files (._prefix)
		if strings.HasPrefix(name, "._") {
			continue
		}
		if !f.IsDir() && strings.HasSuffix(name, ".toml") {
			path := filepath.Join(dir, f.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			var m map[string]string
			if err := toml.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("unmarshal %s: %w", path, err)
			}
			for k, v := range m {
				merged[k] = v
			}
		}
	}

	mu.Lock()
	bundles[loc] = merged
	mu.Unlock()
	return nil
}

// UnloadBundle removes a locale bundle from memory.
// Called when a language plugin is uninstalled, freeing memory and ensuring
// subsequent T() calls fall back to en.
func UnloadBundle(loc string) {
	mu.Lock()
	defer mu.Unlock()
	delete(bundles, loc)
}

// HasBundle checks if a locale bundle is loaded.
func HasBundle(loc string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return bundles[loc] != nil
}

// AvailableLocales returns the list of loaded locale codes.
func AvailableLocales() []string {
	mu.RLock()
	defer mu.RUnlock()
	locales := make([]string, 0, len(bundles))
	for loc := range bundles {
		locales = append(locales, loc)
	}
	return locales
}

func loadFromFS(loc string, fs embed.FS, path string) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return
	}
	var m map[string]string
	if err := toml.Unmarshal(data, &m); err != nil {
		return
	}
	mu.Lock()
	bundles[loc] = m
	mu.Unlock()
}