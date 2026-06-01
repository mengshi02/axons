package extractors

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

func TestPythonExtractor_Functions(t *testing.T) {
	source := []byte(`
def public_func(x, y):
    return x + y

def _protected_func():
    pass

def __private_func():
    pass
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "public_func")
	assertContains(t, funcNames, "_protected_func")
	assertContains(t, funcNames, "__private_func")

	for _, def := range output.Definitions {
		switch def.Name {
		case "public_func":
			if def.Visibility != types.VisibilityPublic {
				t.Errorf("public_func: want Public, got %v", def.Visibility)
			}
			if !def.Exported {
				t.Errorf("public_func: want Exported=true")
			}
		case "_protected_func":
			if def.Visibility != types.VisibilityProtected {
				t.Errorf("_protected_func: want Protected, got %v", def.Visibility)
			}
			if def.Exported {
				t.Errorf("_protected_func: want Exported=false")
			}
		case "__private_func":
			if def.Visibility != types.VisibilityPrivate {
				t.Errorf("__private_func: want Private, got %v", def.Visibility)
			}
		}
	}
}

func TestPythonExtractor_Class(t *testing.T) {
	source := []byte(`
class Base:
    pass

class Child(Base):
    def __init__(self):
        pass

    def method(self):
        pass
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	classNames := definitionNames(output, types.SymbolKindClass)
	assertContains(t, classNames, "Base")
	assertContains(t, classNames, "Child")

	// Bug fix: base class extraction
	found := false
	for _, rel := range output.Classes {
		if rel.ClassName == "Child" && rel.Extends == "Base" {
			found = true
		}
	}
	if !found {
		t.Errorf("Expected Child extends Base, got: %+v", output.Classes)
	}
}

func TestPythonExtractor_MultipleInheritance(t *testing.T) {
	source := []byte(`
class Mixin:
    pass

class Base:
    pass

class Multi(Base, Mixin):
    pass
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, rel := range output.Classes {
		if rel.ClassName == "Multi" {
			if rel.Extends != "Base" {
				t.Errorf("Expected Multi extends Base, got extends=%s", rel.Extends)
			}
			foundMixin := false
			for _, impl := range rel.Implements {
				if impl == "Mixin" {
					foundMixin = true
					break
				}
			}
			if !foundMixin {
				t.Errorf("Expected Multi implements Mixin, got implements=%v", rel.Implements)
			}
		}
	}
}

func TestPythonExtractor_Import(t *testing.T) {
	source := []byte(`
import os
import sys as system
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) != 2 {
		t.Errorf("Expected 2 imports, got %d: %+v", len(output.Imports), output.Imports)
	}
	srcs := importSources(output)
	assertContains(t, srcs, "os")
	assertContains(t, srcs, "sys")

	// Check alias
	for _, imp := range output.Imports {
		if imp.Source == "sys" && imp.Alias != "system" {
			t.Errorf("Expected sys alias=system, got %q", imp.Alias)
		}
	}
}

func TestPythonExtractor_ImportFrom(t *testing.T) {
	// Bug fix test: from x import a, b as c  (items as direct children, not in import_list)
	source := []byte(`
from os.path import join, dirname as dname
from collections import OrderedDict
from sys import *
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Expect: join (alias=join), dirname->dname, OrderedDict, * from sys
	if len(output.Imports) != 4 {
		t.Errorf("Expected 4 imports, got %d: %+v", len(output.Imports), output.Imports)
	}

	aliasMap := map[string]string{}
	for _, imp := range output.Imports {
		aliasMap[imp.Alias] = imp.Source
	}

	if aliasMap["join"] == "" {
		t.Errorf("Expected import with alias=join")
	}
	if aliasMap["dname"] != "os.path.dirname" {
		t.Errorf("Expected alias dname -> os.path.dirname, got %q", aliasMap["dname"])
	}
	if aliasMap["OrderedDict"] == "" {
		t.Errorf("Expected import with alias=OrderedDict")
	}
}

func TestPythonExtractor_Calls(t *testing.T) {
	source := []byte(`
def main():
    print("hello")
    obj.method(1, 2)
    result = len(items)
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	names := callNameList(output)
	assertContains(t, names, "print")
	assertContains(t, names, "method")
	assertContains(t, names, "len")

	for _, c := range output.Calls {
		if c.Name == "method" {
			if !c.IsMethod {
				t.Errorf("method: want IsMethod=true")
			}
			if c.Receiver != "obj" {
				t.Errorf("method: want Receiver=obj, got %q", c.Receiver)
			}
		}
	}
}

func TestPythonExtractor_ModuleConstants(t *testing.T) {
	source := []byte(`
MAX_SIZE = 100
DEBUG = True
version = "1.0"
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	constNames := definitionNames(output, types.SymbolKindConstant)
	assertContains(t, constNames, "MAX_SIZE")
	// lowercase 'version' should NOT be emitted as constant (noise reduction)
	for _, name := range constNames {
		if name == "version" {
			t.Errorf("'version' should not be extracted as constant (not ALL_CAPS)")
		}
	}
}

func TestPythonExtractor_DunderVisibility(t *testing.T) {
	source := []byte(`
class Foo:
    def __init__(self): pass
    def __str__(self): return ""
    def __secret(self): pass
`)
	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, def := range output.Definitions {
		switch def.Name {
		case "__init__", "__str__":
			// dunder methods: public
			if def.Visibility != types.VisibilityPublic {
				t.Errorf("%s: want Public, got %v", def.Name, def.Visibility)
			}
		case "__secret":
			// name mangled: private
			if def.Visibility != types.VisibilityPrivate {
				t.Errorf("__secret: want Private, got %v", def.Visibility)
			}
		}
	}
}

func TestPythonExtractor_EmptySource(t *testing.T) {
	e := &PythonExtractor{}
	output, err := e.Extract([]byte(""), "empty.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(output.Definitions) != 0 {
		t.Errorf("Expected 0 definitions, got %d", len(output.Definitions))
	}
}

func TestPythonExtractor_Decorators(t *testing.T) {
	source := []byte(`
@dataclass
class User:
    name: str
    age: int

@staticmethod
def static_method():
    pass

@app.route("/api")
def api_handler():
    pass

@auth.required
@login_required
def protected_func():
    pass
`)

	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Check User class has @dataclass decorator
	for _, def := range output.Definitions {
		if def.Name == "User" && def.Kind == types.SymbolKindClass {
			if len(def.Decorators) != 1 {
				t.Errorf("User: want 1 decorator, got %d", len(def.Decorators))
			} else if def.Decorators[0].Name != "dataclass" {
				t.Errorf("User: want decorator 'dataclass', got %q", def.Decorators[0].Name)
			}
		}

		if def.Name == "static_method" {
			if len(def.Decorators) != 1 {
				t.Errorf("static_method: want 1 decorator, got %d", len(def.Decorators))
			} else if def.Decorators[0].Name != "staticmethod" {
				t.Errorf("static_method: want decorator 'staticmethod', got %q", def.Decorators[0].Name)
			}
		}

		if def.Name == "api_handler" {
			if len(def.Decorators) != 1 {
				t.Errorf("api_handler: want 1 decorator, got %d", len(def.Decorators))
			} else {
				if def.Decorators[0].Name != "app.route" {
					t.Errorf("api_handler: want decorator name 'app.route', got %q", def.Decorators[0].Name)
				}
				if def.Decorators[0].Args != `("/api")` {
					t.Errorf("api_handler: want args %q, got %q", `("/api")`, def.Decorators[0].Args)
				}
			}
		}

		if def.Name == "protected_func" {
			if len(def.Decorators) != 2 {
				t.Errorf("protected_func: want 2 decorators, got %d", len(def.Decorators))
			}
		}
	}
}

func TestPythonExtractor_AsyncFunctions(t *testing.T) {
	source := []byte(`
async def fetch_data():
    pass

@asyncio.coroutine
async def fetch_all():
    pass
`)

	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	asyncCount := 0
	for _, def := range output.Definitions {
		if def.Name == "fetch_data" || def.Name == "fetch_all" {
			if !def.IsAsync {
				t.Errorf("%s: want IsAsync=true", def.Name)
			}
			asyncCount++
		}
	}
	if asyncCount != 2 {
		t.Errorf("want 2 async functions, got %d", asyncCount)
	}
}

func TestPythonExtractor_TypeAnnotations(t *testing.T) {
	source := []byte(`
def greet(name: str) -> str:
    return f"Hello, {name}"

def process(data: list[int], count: int = 10) -> bool:
    return True

class User:
    name: str
    age: int = 0
`)

	e := &PythonExtractor{}
	output, err := e.Extract(source, "test.py")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, def := range output.Definitions {
		if def.Name == "greet" {
			if def.TypeSignature != "(name: str) -> str" {
				t.Errorf("greet: want TypeSignature %q, got %q", "(name: str) -> str", def.TypeSignature)
			}
		}
		if def.Name == "process" {
			expected := "(data: list[int], count: int = 10) -> bool"
			if def.TypeSignature != expected {
				t.Errorf("process: want TypeSignature %q, got %q", expected, def.TypeSignature)
			}
		}
	}
}