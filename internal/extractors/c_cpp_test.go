package extractors

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

// ---- C Extractor Tests ----

func TestCExtractor_Functions(t *testing.T) {
	source := []byte(`
#include <stdio.h>

int add(int a, int b) {
    return a + b;
}

void greet(const char* name) {
    printf("Hello, %s\n", name);
}

static int helper(void) {
    return 0;
}
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "add")
	assertContains(t, funcNames, "greet")
	assertContains(t, funcNames, "helper")

	if len(funcNames) != 3 {
		t.Errorf("Expected 3 functions, got %d: %v", len(funcNames), funcNames)
	}
}

func TestCExtractor_Include(t *testing.T) {
	source := []byte(`
#include <stdio.h>
#include <stdlib.h>
#include "myheader.h"

int main(void) {
    return 0;
}
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) != 3 {
		t.Errorf("Expected 3 imports, got %d", len(output.Imports))
	}

	sources := importSources(output)
	assertContains(t, sources, "stdio.h")
	assertContains(t, sources, "stdlib.h")
	assertContains(t, sources, "myheader.h")
}

func TestCExtractor_Struct(t *testing.T) {
	source := []byte(`
struct Point {
    int x;
    int y;
};

struct Node {
    int value;
    struct Node* next;
};
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	structNames := definitionNames(output, types.SymbolKindStruct)
	assertContains(t, structNames, "Point")
	assertContains(t, structNames, "Node")
}

func TestCExtractor_Enum(t *testing.T) {
	source := []byte(`
enum Color {
    RED,
    GREEN,
    BLUE
};

enum Direction {
    NORTH,
    SOUTH,
    EAST,
    WEST
};
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	enumNames := definitionNames(output, types.SymbolKindEnum)
	assertContains(t, enumNames, "Color")
	assertContains(t, enumNames, "Direction")
}

func TestCExtractor_Typedef(t *testing.T) {
	source := []byte(`
typedef struct {
    int x;
    int y;
} Point;

typedef unsigned int uint32;
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	typeNames := definitionNames(output, types.SymbolKindType)
	assertContains(t, typeNames, "Point")
	assertContains(t, typeNames, "uint32")
}

func TestCExtractor_FunctionCalls(t *testing.T) {
	source := []byte(`
#include <stdio.h>

int main(void) {
    printf("hello\n");
    int x = add(1, 2);
    return 0;
}
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	callNames := callNameList(output)
	assertContains(t, callNames, "printf")
	assertContains(t, callNames, "add")
}

func TestCExtractor_EmptySource(t *testing.T) {
	e := &CExtractor{}
	output, err := e.Extract([]byte(""), "empty.c")
	if err != nil {
		t.Fatalf("Extract failed on empty source: %v", err)
	}
	if len(output.Definitions) != 0 {
		t.Errorf("Expected 0 definitions for empty source, got %d", len(output.Definitions))
	}
}

// ---- C++ Extractor Tests ----

func TestCppExtractor_Class(t *testing.T) {
	source := []byte(`
class Animal {
public:
    Animal(const char* name);
    virtual void speak() = 0;
    virtual ~Animal() {}
private:
    const char* name_;
};

class Dog : public Animal {
public:
    Dog(const char* name);
    void speak() override;
};
`)
	e := &CppExtractor{}
	output, err := e.Extract(source, "test.cpp")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	classNames := definitionNames(output, types.SymbolKindClass)
	assertContains(t, classNames, "Animal")
	assertContains(t, classNames, "Dog")

	// Dog should extend Animal
	found := false
	for _, rel := range output.Classes {
		if rel.ClassName == "Dog" && rel.Extends == "Animal" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected Dog extends Animal relation, got: %+v", output.Classes)
	}
}

func TestCppExtractor_Methods(t *testing.T) {
	source := []byte(`
#include <string>

class Calculator {
public:
    int add(int a, int b);
    int subtract(int a, int b);
};

int Calculator::add(int a, int b) {
    return a + b;
}

int Calculator::subtract(int a, int b) {
    return a - b;
}
`)
	e := &CppExtractor{}
	output, err := e.Extract(source, "test.cpp")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	methodNames := definitionNames(output, types.SymbolKindMethod)
	assertContains(t, methodNames, "add")
	assertContains(t, methodNames, "subtract")

	// Verify parent class
	for _, def := range output.Definitions {
		if def.Kind == types.SymbolKindMethod && def.Name == "add" {
			if def.Parent != "Calculator" {
				t.Errorf("Expected method 'add' parent to be 'Calculator', got '%s'", def.Parent)
			}
		}
	}
}

func TestCppExtractor_Namespace(t *testing.T) {
	source := []byte(`
namespace math {
    int add(int a, int b) {
        return a + b;
    }
}

namespace utils {
    void log(const char* msg) {}
}
`)
	e := &CppExtractor{}
	output, err := e.Extract(source, "test.cpp")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	nsNames := definitionNames(output, types.SymbolKindModule)
	assertContains(t, nsNames, "math")
	assertContains(t, nsNames, "utils")
}

func TestCppExtractor_Template(t *testing.T) {
	source := []byte(`
template<typename T>
T max(T a, T b) {
    return a > b ? a : b;
}

template<typename T>
class Stack {
public:
    void push(T item);
    T pop();
private:
    T data_[100];
    int top_;
};
`)
	e := &CppExtractor{}
	output, err := e.Extract(source, "test.cpp")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "max")

	classNames := definitionNames(output, types.SymbolKindClass)
	assertContains(t, classNames, "Stack")
}

func TestCppExtractor_Include(t *testing.T) {
	source := []byte(`
#include <iostream>
#include <vector>
#include "myclass.h"

int main() {
    return 0;
}
`)
	e := &CppExtractor{}
	output, err := e.Extract(source, "test.cpp")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) != 3 {
		t.Errorf("Expected 3 imports, got %d", len(output.Imports))
	}
	sources := importSources(output)
	assertContains(t, sources, "iostream")
	assertContains(t, sources, "vector")
	assertContains(t, sources, "myclass.h")
}

func TestRegistryDetectCLanguages(t *testing.T) {
	reg := NewRegistry()

	cases := []struct {
		file   string
		wantID string
	}{
		{"main.c", "c"},
		{"utils.h", "c"},
		{"inline.inl", "c"},
		{"config.inc", "c"},
		{"app.cpp", "cpp"},
		{"app.cc", "cpp"},
		{"header.hpp", "cpp"},
	}

	for _, tc := range cases {
		lang := reg.DetectLanguage(tc.file)
		if lang == nil {
			t.Errorf("DetectLanguage(%q) returned nil, expected %q", tc.file, tc.wantID)
			continue
		}
		if lang.ID != tc.wantID {
			t.Errorf("DetectLanguage(%q) = %q, want %q", tc.file, lang.ID, tc.wantID)
		}
	}
}

// ---- Helpers ----

func definitionNames(output *types.ExtractorOutput, kind types.SymbolKind) []string {
	var names []string
	for _, d := range output.Definitions {
		if d.Kind == kind {
			names = append(names, d.Name)
		}
	}
	return names
}

func importSources(output *types.ExtractorOutput) []string {
	var srcs []string
	for _, imp := range output.Imports {
		srcs = append(srcs, imp.Source)
	}
	return srcs
}

func callNameList(output *types.ExtractorOutput) []string {
	var names []string
	for _, c := range output.Calls {
		names = append(names, c.Name)
	}
	return names
}

func assertContains(t *testing.T, list []string, item string) {
	t.Helper()
	for _, v := range list {
		if v == item {
			return
		}
	}
	t.Errorf("Expected %q in %v, but not found", item, list)
}

// ---- New C Extractor Tests: Preproc Define, Macro Functions, Global Variables ----

func TestCExtractor_PreprocDefine(t *testing.T) {
	source := []byte(`
#define MAX_CONNECTIONS 1000
#define BUFFER_SIZE 4096
#define VERSION "1.0.0"
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	constNames := definitionNames(output, types.SymbolKindConstant)
	assertContains(t, constNames, "MAX_CONNECTIONS")
	assertContains(t, constNames, "BUFFER_SIZE")
	assertContains(t, constNames, "VERSION")

	if len(constNames) != 3 {
		t.Errorf("Expected 3 constants, got %d: %v", len(constNames), constNames)
	}
}

func TestCExtractor_PreprocFunctionDef(t *testing.T) {
	source := []byte(`
#define MAX(a, b) ((a) > (b) ? (a) : (b))
#define MIN(a, b) ((a) < (b) ? (a) : (b))
#define MAKE_STRING(x) #x
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	macroNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, macroNames, "MAX")
	assertContains(t, macroNames, "MIN")
	assertContains(t, macroNames, "MAKE_STRING")

	if len(macroNames) != 3 {
		t.Errorf("Expected 3 macro functions, got %d: %v", len(macroNames), macroNames)
	}
}

func TestCExtractor_GlobalVariables(t *testing.T) {
	source := []byte(`
#include <stdio.h>

int global_counter = 0;
static const char* version = "1.0";
char* buffer[256];
struct redisServer server;
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	varNames := definitionNames(output, types.SymbolKindVariable)
	assertContains(t, varNames, "global_counter")
	assertContains(t, varNames, "version")
	assertContains(t, varNames, "buffer")
	assertContains(t, varNames, "server")

	if len(varNames) < 4 {
		t.Errorf("Expected at least 4 global variables, got %d: %v", len(varNames), varNames)
	}
}

func TestCExtractor_RedisLikePatterns(t *testing.T) {
	source := []byte(`
#define OBJ_STRING 0
#define OBJ_LIST 1
#define OBJ_SET 2
#define ZSKIPLIST_MAXLEVEL 32
#define ZSKIPLIST_P 0.25

#define dictGetKey(d) ((d)->key)
#define dictGetVal(d) ((d)->v.val)

struct redisServer server;
struct sharedObjectsStruct shared;
int server_port = 6379;

void initServer(void) {
    server.port = server_port;
}
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Check #define constants
	constNames := definitionNames(output, types.SymbolKindConstant)
	assertContains(t, constNames, "OBJ_STRING")
	assertContains(t, constNames, "OBJ_LIST")
	assertContains(t, constNames, "OBJ_SET")
	assertContains(t, constNames, "ZSKIPLIST_MAXLEVEL")
	assertContains(t, constNames, "ZSKIPLIST_P")

	// Check macro functions
	macroNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, macroNames, "dictGetKey")
	assertContains(t, macroNames, "dictGetVal")

	// Check global variables
	varNames := definitionNames(output, types.SymbolKindVariable)
	assertContains(t, varNames, "server")
	assertContains(t, varNames, "shared")
	assertContains(t, varNames, "server_port")

	// Check regular function
	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "initServer")
}

func TestCExtractor_FunctionForwardDeclarations(t *testing.T) {
	source := []byte(`
void ngx_init_cycle(ngx_cycle_t *cycle);
static ngx_int_t ngx_process_events(ngx_cycle_t *cycle);
ngx_int_t ngx_open_file(ngx_str_t *name, ngx_uint_t mode);
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "ngx_init_cycle")
	assertContains(t, funcNames, "ngx_process_events")
	assertContains(t, funcNames, "ngx_open_file")
}

func TestCExtractor_DeclarationsInPreprocBlocks(t *testing.T) {
	source := []byte(`
#ifdef NGX_DEBUG
int ngx_debug_enabled = 1;
ngx_int_t ngx_debug_level = 3;
#else
int ngx_debug_enabled = 0;
#endif

#if (NGX_HAVE_EPOLL)
ngx_event_actions_t ngx_epoll_actions;
#endif
`)
	e := &CExtractor{}
	output, err := e.Extract(source, "test.c")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	varNames := definitionNames(output, types.SymbolKindVariable)
	assertContains(t, varNames, "ngx_debug_enabled")
	assertContains(t, varNames, "ngx_debug_level")
	assertContains(t, varNames, "ngx_epoll_actions")
}