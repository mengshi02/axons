// Package graph provides code graph building and querying capabilities.
package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mengshi02/axons/pkg/types"
)

// PathAlias represents path alias configuration from tsconfig/jsconfig.
type PathAlias struct {
	BaseURL string              `json:"baseUrl"`
	Paths   map[string][]string `json:"paths"`
}

// AliasResolver resolves import paths using path aliases.
type AliasResolver struct {
	rootDir    string
	aliases    []aliasMapping
	workspaces []string
}

type aliasMapping struct {
	pattern     *regexp.Regexp
	replacement string
	prefix      string
}

// NewAliasResolver creates a new alias resolver.
func NewAliasResolver(rootDir string) *AliasResolver {
	return &AliasResolver{
		rootDir: rootDir,
		aliases: make([]aliasMapping, 0),
	}
}

// LoadFromTSConfig loads path aliases from tsconfig.json.
func (r *AliasResolver) LoadFromTSConfig() error {
	tsconfigPath := filepath.Join(r.rootDir, "tsconfig.json")
	data, err := os.ReadFile(tsconfigPath)
	if err != nil {
		// Try jsconfig.json
		jsconfigPath := filepath.Join(r.rootDir, "jsconfig.json")
		data, err = os.ReadFile(jsconfigPath)
		if err != nil {
			return nil // No config file, that's okay
		}
	}

	var config struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	baseURL := config.CompilerOptions.BaseURL
	for pattern, paths := range config.CompilerOptions.Paths {
		for _, path := range paths {
			r.addAlias(pattern, path, baseURL)
		}
	}

	return nil
}

// LoadFromPackageJSON loads workspaces from package.json.
func (r *AliasResolver) LoadFromPackageJSON() error {
	packagePath := filepath.Join(r.rootDir, "package.json")
	data, err := os.ReadFile(packagePath)
	if err != nil {
		return nil
	}

	var pkg struct {
		Workspaces interface{} `json:"workspaces"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	switch v := pkg.Workspaces.(type) {
	case []interface{}:
		for _, w := range v {
			if ws, ok := w.(string); ok {
				r.workspaces = append(r.workspaces, ws)
			}
		}
	case map[string]interface{}:
		if packages, ok := v["packages"].([]interface{}); ok {
			for _, p := range packages {
				if pkg, ok := p.(string); ok {
					r.workspaces = append(r.workspaces, pkg)
				}
			}
		}
	}

	return nil
}

func (r *AliasResolver) addAlias(pattern, replacement, baseURL string) {
	// Normalize paths
	pattern = strings.TrimSuffix(pattern, "*")
	replacement = strings.TrimSuffix(replacement, "*")

	// Prepend baseURL if relative
	if !filepath.IsAbs(replacement) && baseURL != "" {
		replacement = filepath.Join(baseURL, replacement)
	}

	// Create regex pattern for matching
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "(.*)$"

	r.aliases = append(r.aliases, aliasMapping{
		pattern:     regexp.MustCompile(regexPattern),
		replacement: replacement,
		prefix:      pattern,
	})
}

// Resolve resolves an import path to a file path.
func (r *AliasResolver) Resolve(importPath, fromFile string) (string, float64, error) {
	// 1. Try path alias resolution
	if resolved, confidence, err := r.resolveAlias(importPath); err == nil {
		return resolved, confidence, nil
	}

	// 2. Try relative path resolution
	if strings.HasPrefix(importPath, ".") || strings.HasPrefix(importPath, "/") {
		resolved := r.resolveRelative(importPath, fromFile)
		return resolved, 1.0, nil
	}

	// 3. Try workspace resolution
	if resolved, confidence, err := r.resolveWorkspace(importPath); err == nil {
		return resolved, confidence, nil
	}

	// 4. Return as-is for node_modules (external dependency)
	return importPath, 0.5, nil
}

func (r *AliasResolver) resolveAlias(importPath string) (string, float64, error) {
	for _, alias := range r.aliases {
		if alias.pattern.MatchString(importPath) {
			matches := alias.pattern.FindStringSubmatch(importPath)
			if len(matches) > 1 {
				resolved := filepath.Join(alias.replacement, matches[1])
				return resolved, 1.0, nil
			}
		}
	}
	return "", 0, fmt.Errorf("no matching alias")
}

func (r *AliasResolver) resolveRelative(importPath, fromFile string) string {
	fromDir := filepath.Dir(fromFile)
	resolved := filepath.Join(fromDir, importPath)
	return filepath.Clean(resolved)
}

func (r *AliasResolver) resolveWorkspace(importPath string) (string, float64, error) {
	// Check if import matches any workspace pattern
	for _, ws := range r.workspaces {
		// Handle glob patterns like "packages/*"
		if strings.Contains(ws, "*") {
			wsDir := strings.TrimSuffix(ws, "*")
			possiblePath := filepath.Join(wsDir, importPath)
			fullPath := filepath.Join(r.rootDir, possiblePath)
			if _, err := os.Stat(fullPath); err == nil {
				return possiblePath, 0.8, nil
			}
		}
	}
	return "", 0, fmt.Errorf("not a workspace import")
}

// ImportResolver resolves imports for the graph builder.
type ImportResolver struct {
	aliasResolver *AliasResolver
	nodeModules   map[string]string   // package name -> resolved path
	reexportMap   map[string][]string // file -> exported symbols
}

// NewImportResolver creates a new import resolver.
func NewImportResolver(rootDir string) *ImportResolver {
	return &ImportResolver{
		aliasResolver: NewAliasResolver(rootDir),
		nodeModules:   make(map[string]string),
		reexportMap:   make(map[string][]string),
	}
}

// Initialize loads alias configurations.
func (r *ImportResolver) Initialize() error {
	if err := r.aliasResolver.LoadFromTSConfig(); err != nil {
		return err
	}
	if err := r.aliasResolver.LoadFromPackageJSON(); err != nil {
		return err
	}
	return nil
}

// ResolveImport resolves a single import statement.
func (r *ImportResolver) ResolveImport(imp types.Import, fromFile string) (ResolvedImport, error) {
	resolved := ResolvedImport{
		OriginalPath: imp.Source,
		Line:         imp.Line,
		IsNamed:      imp.IsNamed,
		IsDefault:    imp.IsDefault,
		IsType:       imp.IsType,
		IsDynamic:    imp.IsDynamic,
		Alias:        imp.Alias,
	}

	// Resolve the path
	path, confidence, err := r.aliasResolver.Resolve(imp.Source, fromFile)
	if err != nil {
		resolved.Unresolved = true
		resolved.Confidence = 0
		return resolved, nil
	}

	resolved.ResolvedPath = path
	resolved.Confidence = confidence

	// Try to find actual file
	possibleFiles := r.getPossibleFiles(path)
	for _, file := range possibleFiles {
		if _, err := os.Stat(file); err == nil {
			resolved.ResolvedFile = file
			break
		}
	}

	return resolved, nil
}

// ResolveImportsBatch resolves multiple imports in batch for efficiency.
func (r *ImportResolver) ResolveImportsBatch(imports []types.Import, fromFile string) ([]ResolvedImport, error) {
	results := make([]ResolvedImport, len(imports))
	for i, imp := range imports {
		resolved, err := r.ResolveImport(imp, fromFile)
		if err != nil {
			results[i] = ResolvedImport{
				OriginalPath: imp.Source,
				Unresolved:   true,
			}
			continue
		}
		results[i] = resolved
	}
	return results, nil
}

// getPossibleFiles returns possible file paths for an import.
func (r *ImportResolver) getPossibleFiles(importPath string) []string {
	extensions := []string{".ts", ".tsx", ".js", ".jsx", ".go", ".py", ".rs", ".java", ".cs", ".php", ".rb"}
	var files []string

	// Direct file
	for _, ext := range extensions {
		files = append(files, importPath+ext)
	}

	// Index file
	for _, ext := range extensions {
		files = append(files, filepath.Join(importPath, "index"+ext))
	}

	return files
}

// RegisterReexport registers a re-export from a file.
func (r *ImportResolver) RegisterReexport(file string, exportedSymbols []string) {
	r.reexportMap[file] = append(r.reexportMap[file], exportedSymbols...)
}

// ResolveReexport resolves re-exports (barrel files).
func (r *ImportResolver) ResolveReexport(file string) []string {
	return r.reexportMap[file]
}

// ResolvedImport represents a resolved import.
type ResolvedImport struct {
	OriginalPath string  `json:"original_path"`
	ResolvedPath string  `json:"resolved_path,omitempty"`
	ResolvedFile string  `json:"resolved_file,omitempty"`
	Confidence   float64 `json:"confidence"`
	Line         int     `json:"line"`
	IsNamed      bool    `json:"is_named"`
	IsDefault    bool    `json:"is_default"`
	IsType       bool    `json:"is_type"`
	IsDynamic    bool    `json:"is_dynamic"`
	Alias        string  `json:"alias,omitempty"`
	Unresolved   bool    `json:"unresolved,omitempty"`
}

// ResolutionPriority defines the 6-level resolution priority.
type ResolutionPriority int

const (
	// PriorityImportAware is for imports with explicit import statements (confidence 1.0).
	PriorityImportAware ResolutionPriority = iota
	// PrioritySameFile is for definitions in the same file (confidence 1.0).
	PrioritySameFile
	// PrioritySameDirectory is for definitions in the same directory (confidence 0.7).
	PrioritySameDirectory
	// PriorityParentDirectory is for definitions in parent directories (confidence 0.5).
	PriorityParentDirectory
	// PriorityMethodHierarchy is for method resolution through extends/implements.
	PriorityMethodHierarchy
	// PriorityGlobalFallback is for global fallback search (confidence 0.3).
	PriorityGlobalFallback
)

// NodeRepository interface for call resolver.
type NodeRepository interface {
	FindNodesByName(name string, limit int) ([]*types.Node, error)
}

// CallResolver resolves a call expression to a function definition.
type CallResolver struct {
	repo           NodeRepository
	importResolver *ImportResolver
	nodesByName    map[string][]*types.Node // name -> nodes
	nodesByFile    map[string][]*types.Node // file -> nodes
	classHierarchy map[string][]string      // class -> implemented interfaces
}

// NewCallResolver creates a new call resolver.
func NewCallResolver(repo NodeRepository, importResolver *ImportResolver) *CallResolver {
	return &CallResolver{
		repo:           repo,
		importResolver: importResolver,
		nodesByName:    make(map[string][]*types.Node),
		nodesByFile:    make(map[string][]*types.Node),
		classHierarchy: make(map[string][]string),
	}
}

// BuildIndex builds the node index for fast lookup.
func (r *CallResolver) BuildIndex(nodes []*types.Node) {
	for _, node := range nodes {
		r.nodesByName[node.Name] = append(r.nodesByName[node.Name], node)
		r.nodesByFile[node.File] = append(r.nodesByFile[node.File], node)
	}
}

// ResolveCall resolves a call to target nodes.
func (r *CallResolver) ResolveCall(call types.Call, fromFile string, nodesInFile []*types.Node) ([]*types.Node, float64) {
	var candidates []*types.Node
	var confidence float64

	// 1. Check same file definitions first
	for _, node := range nodesInFile {
		if node.Name == call.Name {
			candidates = append(candidates, node)
			confidence = 1.0
		}
	}

	if len(candidates) > 0 {
		return candidates, confidence
	}

	// 2. Check by receiver for method calls
	if call.IsMethod && call.Receiver != "" {
		candidates = r.resolveMethodCall(call, fromFile)
		if len(candidates) > 0 {
			return candidates, 0.9
		}
	}

	// 3. Global search
	if nodes, ok := r.nodesByName[call.Name]; ok {
		// Filter by visibility
		var visible []*types.Node
		for _, node := range nodes {
			if node.Exported || node.Visibility == types.VisibilityPublic {
				visible = append(visible, node)
			}
		}
		if len(visible) > 0 {
			return visible, 0.7
		}
		return nodes, 0.5
	}

	return nil, 0
}

func (r *CallResolver) resolveMethodCall(call types.Call, fromFile string) []*types.Node {
	// Try to find the method in the receiver's class
	var candidates []*types.Node

	// Look for nodes that match the pattern "Receiver.method"
	// Check QualifiedName which contains parent info (e.g., "ClassName.methodName")
	expectedQualified := call.Receiver + "." + call.Name
	if nodes, ok := r.nodesByName[call.Name]; ok {
		for _, node := range nodes {
			if node.Kind == types.SymbolKindMethod {
				// Check if qualified name matches Receiver.Method pattern
				if node.QualifiedName == expectedQualified {
					candidates = append(candidates, node)
				}
			}
		}
	}

	return candidates
}

// NodeRepository is an alias for backward compatibility.
type Repository = NodeRepository
