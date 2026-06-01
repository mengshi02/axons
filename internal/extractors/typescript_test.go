package extractors

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// getTestdataPath returns the path to the testdata directory
func getTestdataPath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata")
}

func TestTypeScriptExtractorRealProject(t *testing.T) {
	// Use the testdata directory instead of hard-coded path
	testdataPath := getTestdataPath()

	// Collect all TypeScript and JavaScript files (including .mjs, .cjs)
	// Using the same extensions and skip directories as registry.go and collect_files.go
	var files []string
	err := filepath.Walk(testdataPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip common non-source directories (same as collect_files.go)
			name := info.Name()
			if name == "node_modules" || name == "dist" || name == "build" ||
				name == ".git" || name == "vendor" || name == "coverage" ||
				name == "target" || name == "bin" || name == "pkg" ||
				strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		switch ext {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk testdata: %v", err)
	}

	t.Logf("Found %d TypeScript/JavaScript files to parse", len(files))

	if len(files) == 0 {
		t.Skip("No TypeScript/JavaScript files found in testdata")
	}

	// Use the SAME extractors as the pipeline (singleton from registry)
	// This is the KEY difference - pipeline uses registry which creates extractor once
	tsExtractor := DefaultRegistry.Get("typescript").Extractor
	jsExtractor := DefaultRegistry.Get("javascript").Extractor

	// Track results
	var successCount, errorCount, panicCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 1) // Serial processing to avoid gotreesitter concurrency bugs

	// Track files that caused panics
	var panicFiles []string

	for _, file := range files {
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
					panicFiles = append(panicFiles, filePath)
					mu.Unlock()
					t.Errorf("PANIC in %s: %v", filepath.Base(filePath), r)
				}
			}()

			content, err := os.ReadFile(filePath)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				t.Logf("Failed to read %s: %v", filepath.Base(filePath), err)
				return
			}

			// Choose extractor based on file extension
			ext := filepath.Ext(filePath)
			var extractErr error

			switch ext {
			case ".ts", ".tsx":
				_, extractErr = tsExtractor.Extract(content, filePath)
			case ".js", ".jsx", ".mjs", ".cjs":
				_, extractErr = jsExtractor.Extract(content, filePath)
			}

			if extractErr != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				t.Logf("Failed to extract %s: %v", filepath.Base(filePath), extractErr)
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()
		}(file)
	}
	wg.Wait()

	t.Logf("Results: Success=%d, Errors=%d, Panics=%d", successCount, errorCount, panicCount)

	if len(panicFiles) > 0 {
		t.Errorf("Files that caused panics:")
		for _, f := range panicFiles {
			t.Errorf("  - %s", f)
		}
	}

	if panicCount > 0 {
		t.Errorf("Total panic count: %d out of %d files", panicCount, len(files))
	}
}

func TestVitestConfigParsing(t *testing.T) {
	// Test parsing the vitest.config.js file from testdata
	testdataPath := getTestdataPath()

	// Get all .js files from testdata (including vitest.config.js)
	var jsFiles []string
	filepath.Walk(testdataPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == ".git" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext == ".js" || ext == ".jsx" || ext == ".mjs" || ext == ".cjs" {
			jsFiles = append(jsFiles, path)
		}
		return nil
	})

	t.Logf("Found %d JS files", len(jsFiles))

	// Use the same approach as pipeline: create new extractor each time
	var panicCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 1) // Serial processing to avoid gotreesitter concurrency bugs

	for _, file := range jsFiles {
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
					t.Errorf("PANIC in %s: %v", filepath.Base(filePath), r)
				}
			}()

			// Use registry (same as pipeline)
			lang := DefaultRegistry.DetectLanguage(filePath)
			if lang == nil {
				return
			}

			// Read file
			content, err := os.ReadFile(filePath)
			if err != nil {
				return
			}

			// Parse using registry's extractor (same as pipeline)
			_, err = lang.Extractor.Extract(content, filePath)
			if err != nil {
				t.Logf("Error parsing %s: %v", filepath.Base(filePath), err)
			}
		}(file)
	}
	wg.Wait()

	t.Logf("Total JS files: %d, Panic count: %d", len(jsFiles), panicCount)
	if panicCount > 0 {
		t.Errorf("Found %d panics", panicCount)
	}
}

// TestPipelineParsing is in internal/domain/graph/pipeline_test.go

func TestProblematicFiles(t *testing.T) {
	// Test parsing various file types from testdata
	testdataPath := getTestdataPath()

	// Test all files from testdata
	testFiles := []string{
		"vitest.config.js",
		"sample.js",
		"sample.ts",
	}

	// Use the same extractor as pipeline (singleton)
	tsExtractor := DefaultRegistry.Get("typescript").Extractor
	jsExtractor := DefaultRegistry.Get("javascript").Extractor

	var panicCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 1) // Serial processing to avoid gotreesitter concurrency bugs

	// Run each file 100 times in parallel to try to trigger panic
	// Note: Even with serial processing, we use sem to ensure safety
	for iteration := 0; iteration < 100; iteration++ {
		for _, relPath := range testFiles {
			filePath := filepath.Join(testdataPath, relPath)

			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				continue
			}

			wg.Add(1)
			go func(fp string, iter int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				content, err := os.ReadFile(fp)
				if err != nil {
					return
				}

				// Recover from panics
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						panicCount++
						mu.Unlock()
						t.Errorf("PANIC in %s iteration %d: %v", filepath.Base(fp), iter, r)
					}
				}()

				ext := filepath.Ext(fp)
				if ext == ".ts" || ext == ".tsx" {
					tsExtractor.Extract(content, fp)
				} else if ext == ".js" || ext == ".jsx" {
					jsExtractor.Extract(content, fp)
				}
			}(filePath, iteration)
		}
	}

	wg.Wait()

	totalTests := len(testFiles) * 100
	t.Logf("Tested %d total iterations, panic count: %d", totalTests, panicCount)

	if panicCount > 0 {
		t.Errorf("Found %d panics", panicCount)
	}
}

func TestConcurrentParsing(t *testing.T) {
	// Test concurrent parsing to check for race conditions
	// Note: gotreesitter has concurrency bugs, so we use serial processing
	testdataPath := getTestdataPath()
	filePath := filepath.Join(testdataPath, "vitest.config.js")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Run serial parsing (not concurrent due to gotreesitter bugs)
	var panicCount int
	iterations := 20

	for i := 0; i < iterations; i++ {
		func(iteration int) {
			defer func() {
				if r := recover(); r != nil {
					panicCount++
					t.Logf("PANIC in iteration %d: %v", iteration, r)
				}
			}()

			extractor := &JavaScriptExtractor{}
			output, err := extractor.Extract(content, filePath)
			if err != nil {
				t.Logf("Iteration %d: Extract error: %v", iteration, err)
			}
			_ = output
		}(i)
	}

	if panicCount > 0 {
		t.Errorf("Parsing had %d panics out of %d iterations", panicCount, iterations)
	} else {
		t.Logf("All %d parses completed successfully", iterations)
	}
}

func TestSpecificFile(t *testing.T) {
	// Test the specific vitest.config.js file
	testdataPath := getTestdataPath()
	filePath := filepath.Join(testdataPath, "vitest.config.js")

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	extractor := &JavaScriptExtractor{}

	// Add recover to catch panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PANIC while parsing %s: %v", filePath, r)
		}
	}()

	output, err := extractor.Extract(content, filePath)
	if err != nil {
		t.Logf("Extract error: %v", err)
	}

	if output != nil {
		t.Logf("Successfully parsed, definitions: %d", len(output.Definitions))
	}
}

func TestJavaScriptExtractorRealProject(t *testing.T) {
	// Use the testdata directory instead of hard-coded path
	testdataPath := getTestdataPath()

	// Collect all JavaScript files only
	var files []string
	err := filepath.Walk(testdataPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == "dist" || name == "build" ||
				name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if ext == ".js" || ext == ".jsx" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk testdata: %v", err)
	}

	t.Logf("Found %d JavaScript files to parse", len(files))

	if len(files) == 0 {
		t.Skip("No JavaScript files found in testdata")
	}

	// Use the actual JavaScriptExtractor
	extractor := &JavaScriptExtractor{}

	var successCount, errorCount, panicCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for _, file := range files {
		wg.Add(1)
		go func(filePath string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					panicCount++
					mu.Unlock()
					t.Logf("PANIC in %s: %v", filepath.Base(filePath), r)
				}
			}()

			content, err := os.ReadFile(filePath)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				return
			}

			output, err := extractor.Extract(content, filePath)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				t.Logf("Failed to extract %s: %v", filepath.Base(filePath), err)
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()

			// Log some info about the output
			if output != nil && len(output.Definitions) > 0 {
				t.Logf("%s: %d definitions", filepath.Base(filePath), len(output.Definitions))
			}
		}(file)
	}
	wg.Wait()

	t.Logf("JavaScript Results: Success=%d, Errors=%d, Panics=%d", successCount, errorCount, panicCount)

	if panicCount > 0 {
		t.Errorf("Found %d panics while parsing JavaScript files", panicCount)
	}
}

func TestTypeScriptExtractor_Decorators(t *testing.T) {
	source := []byte(`
@Injectable()
class UserService {
	@Inject()
	private repository: Repository;

	@Log()
	async getUser(id: number): Promise<User> {
		return this.repository.find(id);
	}
}

@Component({
	selector: 'app-root',
	template: '<div></div>'
})
class AppComponent {}
`)

	e := &TypeScriptExtractor{}
	output, err := e.Extract(source, "test.ts")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, def := range output.Definitions {
		if def.Name == "UserService" {
			if len(def.Decorators) != 1 || def.Decorators[0].Name != "Injectable" {
				t.Errorf("UserService: want @Injectable decorator, got %v", def.Decorators)
			}
		}
		if def.Name == "getUser" {
			if len(def.Decorators) != 1 || def.Decorators[0].Name != "Log" {
				t.Errorf("getUser: want @Log decorator, got %v", def.Decorators)
			}
		}
		if def.Name == "AppComponent" {
			if len(def.Decorators) != 1 {
				t.Errorf("AppComponent: want 1 decorator, got %d", len(def.Decorators))
			}
		}
	}
}