package extractors

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

func TestJavaExtractor_Class(t *testing.T) {
	source := []byte(`
public class Animal {
    private String name;
    public Animal(String n) { this.name = n; }
    public void speak() {}
    protected int count() { return 0; }
    void pkg() {}
    private static int total;
}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Animal.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	classNames := javaDefNames(output, types.SymbolKindClass)
	assertContains(t, classNames, "Animal")

	methodNames := javaDefNames(output, types.SymbolKindMethod)
	assertContains(t, methodNames, "Animal") // constructor
	assertContains(t, methodNames, "speak")
	assertContains(t, methodNames, "count")
	assertContains(t, methodNames, "pkg")

	fields := javaDefNames(output, types.SymbolKindField)
	assertContains(t, fields, "name")
	assertContains(t, fields, "total")
}

func TestJavaExtractor_Visibility(t *testing.T) {
	source := []byte(`
public class Vis {
    public void pub() {}
    protected void prot() {}
    private void priv() {}
    void pkg() {}
}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Vis.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, def := range output.Definitions {
		switch def.Name {
		case "pub":
			if def.Visibility != types.VisibilityPublic {
				t.Errorf("pub: want Public, got %v", def.Visibility)
			}
			if !def.Exported {
				t.Errorf("pub: want Exported=true")
			}
		case "prot":
			if def.Visibility != types.VisibilityProtected {
				t.Errorf("prot: want Protected, got %v", def.Visibility)
			}
		case "priv":
			if def.Visibility != types.VisibilityPrivate {
				t.Errorf("priv: want Private, got %v", def.Visibility)
			}
			if def.Exported {
				t.Errorf("priv: want Exported=false")
			}
		case "pkg":
			if def.Visibility != types.VisibilityInternal {
				t.Errorf("pkg: want Internal, got %v", def.Visibility)
			}
		}
	}
}

func TestJavaExtractor_Inheritance(t *testing.T) {
	source := []byte(`
public class Dog extends Animal implements Runnable, Serializable {
    public void run() {}
}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Dog.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Check extends
	foundExtends := false
	for _, rel := range output.Classes {
		if rel.ClassName == "Dog" && rel.Extends == "Animal" {
			foundExtends = true
		}
	}
	if !foundExtends {
		t.Errorf("Expected Dog extends Animal, got: %+v", output.Classes)
	}

	// Check implements
	implemented := map[string]bool{}
	for _, rel := range output.Classes {
		if rel.ClassName == "Dog" && len(rel.Implements) > 0 {
			for _, iface := range rel.Implements {
				implemented[iface] = true
			}
		}
	}
	if !implemented["Runnable"] {
		t.Errorf("Expected Dog implements Runnable, got: %+v", output.Classes)
	}
	if !implemented["Serializable"] {
		t.Errorf("Expected Dog implements Serializable, got: %+v", output.Classes)
	}
}

func TestJavaExtractor_Interface(t *testing.T) {
	source := []byte(`
public interface Shape {
    void draw();
    double area();
}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Shape.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	ifaceNames := javaDefNames(output, types.SymbolKindInterface)
	assertContains(t, ifaceNames, "Shape")

	methodNames := javaDefNames(output, types.SymbolKindMethod)
	assertContains(t, methodNames, "draw")
	assertContains(t, methodNames, "area")
}

func TestJavaExtractor_Enum(t *testing.T) {
	source := []byte(`
public enum Color {
    RED, GREEN, BLUE;
    public String lower() { return name().toLowerCase(); }
}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Color.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	enumNames := javaDefNames(output, types.SymbolKindEnum)
	assertContains(t, enumNames, "Color")
}

func TestJavaExtractor_Import(t *testing.T) {
	source := []byte(`
import java.util.List;
import java.util.ArrayList;
import com.example.MyClass;

public class Foo {}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Foo.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) != 3 {
		t.Errorf("Expected 3 imports, got %d", len(output.Imports))
	}
	srcs := importSources(output)
	assertContains(t, srcs, "java.util.List")
	assertContains(t, srcs, "java.util.ArrayList")
	assertContains(t, srcs, "com.example.MyClass")
}

func TestJavaExtractor_MethodCall(t *testing.T) {
	source := []byte(`
public class Main {
    public void run() {
        System.out.println("hello");
        list.add(item);
        compute();
    }
}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Main.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	names := callNameList(output)
	assertContains(t, names, "println")
	assertContains(t, names, "add")
	assertContains(t, names, "compute")
}

func TestJavaExtractor_MultipleFields(t *testing.T) {
	// Bug fix test: field_declaration with multiple declarators
	source := []byte(`
public class Multi {
    public int x, y, z;
}
`)
	e := &JavaExtractor{}
	output, err := e.Extract(source, "Multi.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	fields := javaDefNames(output, types.SymbolKindField)
	assertContains(t, fields, "x")
	assertContains(t, fields, "y")
	assertContains(t, fields, "z")
}

func TestJavaExtractor_EmptySource(t *testing.T) {
	e := &JavaExtractor{}
	output, err := e.Extract([]byte(""), "empty.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(output.Definitions) != 0 {
		t.Errorf("Expected 0 definitions, got %d", len(output.Definitions))
	}
}

func TestJavaExtractor_Annotations(t *testing.T) {
	source := []byte(`
@Override
public String toString() {
    return "test";
}

@SuppressWarnings("unchecked")
public void process() {}

@Deprecated
public class OldClass {
    @Autowired
    private Service service;
    
    @Test
    public void testMethod() {}
}

@RequestMapping("/api")
@GetMapping("/users/{id}")
public class ApiController {}
`)

	e := &JavaExtractor{}
	output, err := e.Extract(source, "Test.java")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, def := range output.Definitions {
		if def.Name == "toString" {
			if len(def.Decorators) != 1 || def.Decorators[0].Name != "Override" {
				t.Errorf("toString: want @Override decorator, got %v", def.Decorators)
			}
		}
		if def.Name == "process" {
			if len(def.Decorators) != 1 || def.Decorators[0].Name != "SuppressWarnings" {
				t.Errorf("process: want @SuppressWarnings decorator, got %v", def.Decorators)
			}
		}
		if def.Name == "OldClass" {
			if len(def.Decorators) != 1 || def.Decorators[0].Name != "Deprecated" {
				t.Errorf("OldClass: want @Deprecated decorator, got %v", def.Decorators)
			}
		}
		if def.Name == "service" {
			if len(def.Decorators) != 1 || def.Decorators[0].Name != "Autowired" {
				t.Errorf("service: want @Autowired decorator, got %v", def.Decorators)
			}
		}
		if def.Name == "testMethod" {
			if len(def.Decorators) != 1 || def.Decorators[0].Name != "Test" {
				t.Errorf("testMethod: want @Test decorator, got %v", def.Decorators)
			}
		}
		if def.Name == "ApiController" {
			if len(def.Decorators) != 2 {
				t.Errorf("ApiController: want 2 decorators, got %d", len(def.Decorators))
			}
		}
	}
}

// helper
func javaDefNames(output *types.ExtractorOutput, kind types.SymbolKind) []string {
	return definitionNames(output, kind)
}