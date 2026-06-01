package extractors

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

func TestJavaScriptExtractor_Functions(t *testing.T) {
	source := []byte(`
function regularFunc(x, y) {
	return x + y;
}

const arrowFunc = (a, b) => a + b;

const funcExpr = function namedFunc() {
	return true;
};

export function exportedFunc() {
	return "exported";
}
`)

	e := &JavaScriptExtractor{}
	output, err := e.Extract(source, "test.js")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "regularFunc")
	assertContains(t, funcNames, "namedFunc")
	assertContains(t, funcNames, "exportedFunc")

	// Check exported visibility
	for _, def := range output.Definitions {
		if def.Name == "exportedFunc" {
			if !def.Exported {
				t.Error("exportedFunc should be exported")
			}
			if def.Visibility != types.VisibilityPublic {
				t.Errorf("exportedFunc: want Public visibility, got %v", def.Visibility)
			}
		}
		if def.Name == "regularFunc" {
			if def.Exported {
				t.Error("regularFunc should not be exported")
			}
		}
	}
}

func TestJavaScriptExtractor_Classes(t *testing.T) {
	source := []byte(`
class BaseClass {
	constructor() {
		this.value = 0;
	}

	baseMethod() {
		return this.value;
	}

	#privateMethod() {
		return "private";
	}
}

class ChildClass extends BaseClass {
	constructor(value) {
		super();
		this.value = value;
	}

	childMethod() {
		return super.baseMethod();
	}
}

export class ExportedClass {}
`)

	e := &JavaScriptExtractor{}
	output, err := e.Extract(source, "test.js")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	classNames := definitionNames(output, types.SymbolKindClass)
	assertContains(t, classNames, "BaseClass")
	assertContains(t, classNames, "ChildClass")
	assertContains(t, classNames, "ExportedClass")

	// Check inheritance
	found := false
	for _, rel := range output.Classes {
		if rel.ClassName == "ChildClass" && rel.Extends == "BaseClass" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected ChildClass extends BaseClass, got: %+v", output.Classes)
	}

	// Check methods
	methodNames := definitionNames(output, types.SymbolKindMethod)
	assertContains(t, methodNames, "baseMethod")
	assertContains(t, methodNames, "childMethod")
	assertContains(t, methodNames, "constructor")

	// Check exported class
	for _, def := range output.Definitions {
		if def.Name == "ExportedClass" {
			if !def.Exported {
				t.Error("ExportedClass should be exported")
			}
		}
	}
}

func TestJavaScriptExtractor_Imports(t *testing.T) {
	source := []byte(`
import defaultExport from 'module1';
import { named1, named2 } from 'module2';
import * as namespace from 'module3';
import { specific as alias } from 'module4';
`)

	e := &JavaScriptExtractor{}
	output, err := e.Extract(source, "test.js")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) != 4 {
		t.Errorf("Expected 4 imports, got %d", len(output.Imports))
	}

	// Check default import
	for _, imp := range output.Imports {
		if imp.Source == "module1" {
			if !imp.IsDefault {
				t.Error("module1 import should be default import")
			}
			if imp.Alias != "defaultExport" {
				t.Errorf("Expected alias 'defaultExport', got '%s'", imp.Alias)
			}
		}
		if imp.Source == "module2" {
			if !imp.IsNamed {
				t.Error("module2 import should be named import")
			}
			assertContains(t, imp.Symbols, "named1")
			assertContains(t, imp.Symbols, "named2")
		}
		if imp.Source == "module3" {
			if imp.Alias != "namespace" {
				t.Errorf("Expected alias 'namespace', got '%s'", imp.Alias)
			}
		}
	}
}

func TestJavaScriptExtractor_Calls(t *testing.T) {
	source := []byte(`
function test() {
	regularCall();
	obj.methodCall();
	console.log("test");
	fetchData().then(result => {
		console.log(result);
	});
}
`)

	e := &JavaScriptExtractor{}
	output, err := e.Extract(source, "test.js")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Check for calls
	callNames := make(map[string]bool)
	for _, call := range output.Calls {
		callNames[call.Name] = true
		if call.Name == "methodCall" {
			if !call.IsMethod {
				t.Error("methodCall should be marked as method")
			}
			if call.Receiver != "obj" {
				t.Errorf("Expected receiver 'obj', got '%s'", call.Receiver)
			}
		}
	}

	if !callNames["regularCall"] {
		t.Error("Missing call to 'regularCall'")
	}
	if !callNames["methodCall"] {
		t.Error("Missing call to 'methodCall'")
	}
}

func TestJavaScriptExtractor_Variables(t *testing.T) {
	source := []byte(`
const myVar = 42;
let myLet = "string";
var myVar2 = true;

const funcVar = () => "arrow";
const funcVar2 = function() { return "expr"; };
`)

	e := &JavaScriptExtractor{}
	output, err := e.Extract(source, "test.js")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	varNames := definitionNames(output, types.SymbolKindVariable)
	assertContains(t, varNames, "myVar")
	assertContains(t, varNames, "myLet")
	assertContains(t, varNames, "myVar2")

	// Check function variables
	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "funcVar")
	assertContains(t, funcNames, "funcVar2")
}

func TestJavaScriptExtractor_Exports(t *testing.T) {
	source := []byte(`
export const myConstant = 42;
export function myFunction() {
	return "test";
}
export class MyClass {
	method() {}
}

const internal = "internal";
export { internal as renamed };
`)

	e := &JavaScriptExtractor{}
	output, err := e.Extract(source, "test.js")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	exportNames := make(map[string]bool)
	for _, exp := range output.Exports {
		exportNames[exp.Name] = true
	}

	if !exportNames["myConstant"] {
		t.Error("Missing export 'myConstant'")
	}
	if !exportNames["myFunction"] {
		t.Error("Missing export 'myFunction'")
	}
	if !exportNames["MyClass"] {
		t.Error("Missing export 'MyClass'")
	}
	if !exportNames["internal"] {
		t.Error("Missing export 'internal'")
	}
}