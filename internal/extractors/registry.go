// Package extractor provides source code symbol extraction utilities.
package extractors

import (
	"path/filepath"
	"strings"

	"github.com/mengshi02/axons/pkg/types"
)

// Language represents a supported programming language.
type Language struct {
	ID         string
	Name       string
	Extensions []string
	Extractor  Extractor
}

// Extractor extracts symbols from source code.
type Extractor interface {
	// Extract extracts definitions, calls, imports, etc. from source code.
	Extract(source []byte, filePath string) (*types.ExtractorOutput, error)
}

// Registry holds all registered languages.
type Registry struct {
	languages map[string]*Language
	extMap    map[string]*Language
}

// NewRegistry creates a new language registry.
func NewRegistry() *Registry {
	r := &Registry{
		languages: make(map[string]*Language),
		extMap:    make(map[string]*Language),
	}
	r.registerBuiltins()
	return r
}

// Register registers a language.
func (r *Registry) Register(lang *Language) {
	r.languages[lang.ID] = lang
	for _, ext := range lang.Extensions {
		r.extMap[ext] = lang
		if !strings.HasPrefix(ext, ".") {
			r.extMap["."+ext] = lang
		}
	}
}

// Get gets a language by ID.
func (r *Registry) Get(id string) *Language {
	return r.languages[id]
}

// DetectLanguage detects the language from a file path.
func (r *Registry) DetectLanguage(filePath string) *Language {
	ext := strings.ToLower(filepath.Ext(filePath))
	return r.extMap[ext]
}

// IsSupported checks if a file extension is supported.
func (r *Registry) IsSupported(filePath string) bool {
	return r.DetectLanguage(filePath) != nil
}

// ListLanguages returns all registered languages.
func (r *Registry) ListLanguages() []*types.LanguageInfo {
	var list []*types.LanguageInfo
	for _, lang := range r.languages {
		list = append(list, &types.LanguageInfo{
			ID:         lang.ID,
			Name:       lang.Name,
			Extensions: lang.Extensions,
		})
	}
	return list
}

// registerBuiltins registers all built-in language extractors.
func (r *Registry) registerBuiltins() {
	// JavaScript
	r.Register(&Language{
		ID:         "javascript",
		Name:       "JavaScript",
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		Extractor:  &JavaScriptExtractor{},
	})

	// TypeScript
	r.Register(&Language{
		ID:         "typescript",
		Name:       "TypeScript",
		Extensions: []string{".ts"},
		Extractor:  &TypeScriptExtractor{},
	})

	// TSX
	r.Register(&Language{
		ID:         "tsx",
		Name:       "TSX",
		Extensions: []string{".tsx"},
		Extractor:  &TSXExtractor{},
	})

	// Python
	r.Register(&Language{
		ID:         "python",
		Name:       "Python",
		Extensions: []string{".py", ".pyi"},
		Extractor:  &PythonExtractor{},
	})

	// Go
	r.Register(&Language{
		ID:         "go",
		Name:       "Go",
		Extensions: []string{".go"},
		Extractor:  &GoExtractor{},
	})

	// Rust
	r.Register(&Language{
		ID:         "rust",
		Name:       "Rust",
		Extensions: []string{".rs"},
		Extractor:  &RustExtractor{},
	})

	// Java
	r.Register(&Language{
		ID:         "java",
		Name:       "Java",
		Extensions: []string{".java"},
		Extractor:  &JavaExtractor{},
	})

	// C#
	r.Register(&Language{
		ID:         "csharp",
		Name:       "C#",
		Extensions: []string{".cs"},
		Extractor:  &CSharpExtractor{},
	})

	// C
	r.Register(&Language{
		ID:         "c",
		Name:       "C",
		Extensions: []string{".c", ".h", ".inl", ".inc"},
		Extractor:  &CExtractor{},
	})

	// C++
	r.Register(&Language{
		ID:         "cpp",
		Name:       "C++",
		Extensions: []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"},
		Extractor:  &CppExtractor{},
	})
}


// DefaultRegistry is the default language registry.
var DefaultRegistry = NewRegistry()