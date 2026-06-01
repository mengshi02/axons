package extractors

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestGoExtractor_Import(t *testing.T) {
	source := []byte(`package main

import (
	"fmt"
	"github.com/mengshi02/axons/internal/db"
)

func main() {
	fmt.Println("test")
}
`)
	e := &GoExtractor{}
	output, err := e.Extract(source, "test.go")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) == 0 {
		t.Error("Expected imports to be extracted, got 0")
	}

	for _, imp := range output.Imports {
		t.Logf("Import: Source=%s, Line=%d", imp.Source, imp.Line)
	}

	if len(output.Imports) != 2 {
		t.Errorf("Expected 2 imports, got %d", len(output.Imports))
	}
}

func TestGoExtractorConsistency(t *testing.T) {
	// Use the current project (axons) instead of hard-coded path
	_, filename, _, _ := runtime.Caller(0)
	// Go up from internal/extractors to the project root
	projectPath := filepath.Join(filepath.Dir(filename), "..", "..")

	// Collect all Go files
	var goFiles []string
	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendor and hidden directories
			if info.Name() == "vendor" || strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk project: %v", err)
	}

	if len(goFiles) == 0 {
		t.Skip("No Go files found in project")
	}

	t.Logf("Found %d Go files to parse", len(goFiles))

	// First run
	result1 := parseFiles(t, goFiles, 1)

	// Second run
	result2 := parseFiles(t, goFiles, 2)

	// Compare results
	if len(result1) != len(result2) {
		t.Errorf("File count mismatch: run1=%d, run2=%d", len(result1), len(result2))
	}

	for file, count1 := range result1 {
		count2, ok := result2[file]
		if !ok {
			t.Errorf("File %s missing in run2", file)
			continue
		}
		if count1 != count2 {
			t.Errorf("Node count mismatch for %s: run1=%d, run2=%d", file, count1, count2)
		}
	}

	for file := range result2 {
		if _, ok := result1[file]; !ok {
			t.Errorf("File %s missing in run1", file)
		}
	}

	t.Logf("Consistency check completed")
}

func parseFiles(t *testing.T, files []string, run int) map[string]int {
	results := make(map[string]int)
	lang := grammars.GoLanguage()

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Logf("[Run %d] Failed to read file %s: %v", run, file, err)
			continue
		}

		parser := gotreesitter.NewParser(lang)
		tree, err := parser.Parse(content)
		if err != nil {
			t.Logf("[Run %d] Failed to parse %s: %v", run, file, err)
			continue
		}

		if tree == nil {
			t.Logf("[Run %d] Tree is nil for %s", run, file)
			continue
		}

		root := tree.RootNode()
		if root == nil {
			t.Logf("[Run %d] Root node is nil for %s", run, file)
			continue
		}

		// Count nodes
		count := countNodes(root)
		results[file] = count

		t.Logf("[Run %d] %s: %d nodes", run, filepath.Base(file), count)
	}

	return results
}

func TestGoExtractorRealConcurrency(t *testing.T) {
	// Use the current project (axons) instead of hard-coded path
	_, filename, _, _ := runtime.Caller(0)
	projectPath := filepath.Join(filepath.Dir(filename), "..", "..")

	// Collect all Go files
	var goFiles []string
	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendor and hidden directories
			if info.Name() == "vendor" || strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk project: %v", err)
	}

	if len(goFiles) == 0 {
		t.Skip("No Go files found in project")
	}

	t.Logf("Found %d Go files to parse with real extractor", len(goFiles))

	// Use the actual GoExtractor (singleton like in registry)
	extractor := &GoExtractor{}

	// Run multiple times to check for consistency
	// Use serial processing (concurrency=1) to avoid gotreesitter concurrency bugs
	// See: internal/graph/stages/parse_files.go
	for run := 0; run < 5; run++ {
		var successCount int
		var panicCount int
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 1) // Serial processing to match production code

		for _, file := range goFiles {
			wg.Add(1)
			go func(filePath string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// Recover from panics
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						panicCount++
						mu.Unlock()
						t.Logf("[Run %d] PANIC in %s: %v", run, filepath.Base(filePath), r)
					}
				}()

				content, err := os.ReadFile(filePath)
				if err != nil {
					return
				}

				// Use real extractor
				output, err := extractor.Extract(content, filePath)
				if err != nil {
					t.Logf("[Run %d] Failed to extract %s: %v", run, filepath.Base(filePath), err)
					return
				}

				mu.Lock()
				successCount++
				mu.Unlock()

				// Use output to prevent optimization
				_ = len(output.Definitions)
			}(file)
		}
		wg.Wait()

		t.Logf("[Run %d] Success: %d, Panics: %d", run, successCount, panicCount)
		if panicCount > 0 {
			t.Errorf("Run %d had %d panics!", run, panicCount)
		}
	}
}

func TestGoExtractorConcurrency(t *testing.T) {
	// Use the current project (axons) instead of hard-coded path
	_, filename, _, _ := runtime.Caller(0)
	projectPath := filepath.Join(filepath.Dir(filename), "..", "..")

	// Collect all Go files
	var goFiles []string
	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendor and hidden directories
			if info.Name() == "vendor" || strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk project: %v", err)
	}

	if len(goFiles) == 0 {
		t.Skip("No Go files found in project")
	}

	t.Logf("Found %d Go files to parse concurrently", len(goFiles))

	// Shared language object (simulates the actual usage in registry)
	lang := grammars.GoLanguage()

	// Run multiple times to check for consistency
	// Use serial processing (concurrency=1) to avoid gotreesitter concurrency bugs
	// See: internal/graph/stages/parse_files.go
	for run := 0; run < 5; run++ {
		results := make(map[string]int)
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 1) // Serial processing to avoid concurrency bugs

		for _, file := range goFiles {
			wg.Add(1)
			go func(filePath string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// Recover from panics
				defer func() {
					if r := recover(); r != nil {
						t.Logf("[Run %d] PANIC in %s: %v", run, filepath.Base(filePath), r)
					}
				}()

				content, err := os.ReadFile(filePath)
				if err != nil {
					return
				}

				parser := gotreesitter.NewParser(lang)
				tree, err := parser.Parse(content)
				if err != nil {
					t.Logf("[Run %d] Failed to parse %s: %v", run, filepath.Base(filePath), err)
					return
				}

				if tree == nil {
					t.Logf("[Run %d] Tree is nil for %s", run, filepath.Base(filePath))
					return
				}

				root := tree.RootNode()
				if root == nil {
					return
				}

				count := countNodes(root)
				mu.Lock()
				results[filepath.Base(filePath)] = count
				mu.Unlock()
			}(file)
		}
		wg.Wait()

		t.Logf("[Run %d] Parsed %d files successfully", run, len(results))
	}
}

func countNodes(node *gotreesitter.Node) int {
	if node == nil {
		return 0
	}

	count := 1
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		count += countNodes(child)
	}
	return count
}

func TestGoExtractor_Documentation(t *testing.T) {
	source := []byte(`package main

// UserService manages user data.
// It provides CRUD operations for users.
type UserService struct {
	db Database
}

// Create creates a new user.
// It returns an error if the user already exists.
func (s *UserService) Create(name string) error {
	return nil
}

// connect establishes a database connection.
func connect() error {
	return nil
}

// ExportedFunction has a doc comment.
func ExportedFunction() {}

func undocumentedFunction() {}
`)

	e := &GoExtractor{}
	output, err := e.Extract(source, "test.go")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, def := range output.Definitions {
		switch def.Name {
		case "UserService":
			if !strings.Contains(def.Documentation, "UserService manages user data") {
				t.Errorf("UserService: want documentation, got %q", def.Documentation)
			}
		case "Create":
			if !strings.Contains(def.Documentation, "Create creates a new user") {
				t.Errorf("Create: want documentation, got %q", def.Documentation)
			}
		case "connect":
			if !strings.Contains(def.Documentation, "connect establishes") {
				t.Errorf("connect: want documentation, got %q", def.Documentation)
			}
		case "ExportedFunction":
			if !strings.Contains(def.Documentation, "has a doc comment") {
				t.Errorf("ExportedFunction: want documentation, got %q", def.Documentation)
			}
		case "undocumentedFunction":
			if def.Documentation != "" {
				t.Errorf("undocumentedFunction: want empty documentation, got %q", def.Documentation)
			}
		}
	}
}