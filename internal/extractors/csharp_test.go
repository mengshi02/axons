package extractors

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

func TestCSharpExtractor_Classes(t *testing.T) {
	source := []byte(`
using System;

namespace TestNamespace
{
    public class PublicClass
    {
        private int privateField;
        public string publicField;

        public void PublicMethod()
        {
        }

        private void PrivateMethod()
        {
        }

        protected void ProtectedMethod()
        {
        }

        internal void InternalMethod()
        {
        }
    }

    internal class InternalClass
    {
    }

    class DefaultClass
    {
    }
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	classNames := definitionNames(output, types.SymbolKindClass)
	assertContains(t, classNames, "PublicClass")
	assertContains(t, classNames, "InternalClass")
	assertContains(t, classNames, "DefaultClass")

	// Check visibility
	for _, def := range output.Definitions {
		switch def.Name {
		case "PublicClass":
			if !def.Exported {
				t.Error("PublicClass should be exported")
			}
			if def.Visibility != types.VisibilityPublic {
				t.Errorf("PublicClass: want Public, got %v", def.Visibility)
			}
		case "InternalClass":
			if def.Visibility != types.VisibilityInternal {
				t.Errorf("InternalClass: want Internal, got %v", def.Visibility)
			}
		case "DefaultClass":
			if def.Visibility != types.VisibilityPrivate {
				t.Errorf("DefaultClass: want Private, got %v", def.Visibility)
			}
		}
	}
}

func TestCSharpExtractor_Methods(t *testing.T) {
	source := []byte(`
public class TestClass
{
    public void PublicMethod() { }
    private void PrivateMethod() { }
    protected void ProtectedMethod() { }
    internal void InternalMethod() { }
    protected internal void ProtectedInternalMethod() { }
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	methodNames := definitionNames(output, types.SymbolKindMethod)
	assertContains(t, methodNames, "PublicMethod")
	assertContains(t, methodNames, "PrivateMethod")
	assertContains(t, methodNames, "ProtectedMethod")
	assertContains(t, methodNames, "InternalMethod")
	assertContains(t, methodNames, "ProtectedInternalMethod")

	// Check visibility
	for _, def := range output.Definitions {
		switch def.Name {
		case "PublicMethod":
			if def.Visibility != types.VisibilityPublic {
				t.Errorf("PublicMethod: want Public, got %v", def.Visibility)
			}
		case "PrivateMethod":
			if def.Visibility != types.VisibilityPrivate {
				t.Errorf("PrivateMethod: want Private, got %v", def.Visibility)
			}
		case "ProtectedMethod":
			if def.Visibility != types.VisibilityProtected {
				t.Errorf("ProtectedMethod: want Protected, got %v", def.Visibility)
			}
		case "InternalMethod":
			if def.Visibility != types.VisibilityInternal {
				t.Errorf("InternalMethod: want Internal, got %v", def.Visibility)
			}
		}
	}
}

func TestCSharpExtractor_Interface(t *testing.T) {
	source := []byte(`
public interface IRepository
{
    void Save();
    TEntity GetById(int id);
}

internal class Repository : IRepository
{
    public void Save() { }
    public TEntity GetById(int id) { return default; }
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	interfaceNames := definitionNames(output, types.SymbolKindInterface)
	assertContains(t, interfaceNames, "IRepository")

	for _, def := range output.Definitions {
		if def.Name == "IRepository" {
			if !def.Exported {
				t.Error("IRepository should be exported")
			}
			if def.Visibility != types.VisibilityPublic {
				t.Errorf("IRepository: want Public, got %v", def.Visibility)
			}
		}
	}
}

func TestCSharpExtractor_Enum(t *testing.T) {
	source := []byte(`
public enum Status
{
    Active,
    Inactive,
    Pending
}

internal enum InternalStatus
{
    Running,
    Stopped
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	enumNames := definitionNames(output, types.SymbolKindEnum)
	assertContains(t, enumNames, "Status")
	assertContains(t, enumNames, "InternalStatus")

	for _, def := range output.Definitions {
		if def.Name == "Status" {
			if !def.Exported {
				t.Error("Status enum should be exported")
			}
		}
	}
}

func TestCSharpExtractor_Struct(t *testing.T) {
	source := []byte(`
public struct Point
{
    public int X;
    public int Y;

    public Point(int x, int y)
    {
        X = x;
        Y = y;
    }
}

internal struct InternalStruct
{
    private int value;
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	structNames := definitionNames(output, types.SymbolKindStruct)
	assertContains(t, structNames, "Point")
	assertContains(t, structNames, "InternalStruct")

	for _, def := range output.Definitions {
		if def.Name == "Point" {
			if !def.Exported {
				t.Error("Point struct should be exported")
			}
		}
	}
}

func TestCSharpExtractor_Imports(t *testing.T) {
	source := []byte(`
using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.EntityFrameworkCore;

namespace MyApp
{
    class Program
    {
        static void Main(string[] args)
        {
        }
    }
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) < 3 {
		t.Errorf("Expected at least 3 imports, got %d", len(output.Imports))
	}

	importSources := make(map[string]bool)
	for _, imp := range output.Imports {
		importSources[imp.Source] = true
	}

	if !importSources["System"] {
		t.Error("Missing import 'System'")
	}
	if !importSources["System.Collections.Generic"] {
		t.Error("Missing import 'System.Collections.Generic'")
	}
	if !importSources["System.Linq"] {
		t.Error("Missing import 'System.Linq'")
	}
}

func TestCSharpExtractor_Fields(t *testing.T) {
	source := []byte(`
public class Entity
{
    private int id;
    protected string name;
    public DateTime createdAt;
    internal decimal balance;
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	fieldNames := definitionNames(output, types.SymbolKindField)
	assertContains(t, fieldNames, "id")
	assertContains(t, fieldNames, "name")
	assertContains(t, fieldNames, "createdAt")
	assertContains(t, fieldNames, "balance")

	for _, def := range output.Definitions {
		if def.Name == "id" && def.Visibility != types.VisibilityPrivate {
			t.Errorf("id field: want Private, got %v", def.Visibility)
		}
		if def.Name == "name" && def.Visibility != types.VisibilityProtected {
			t.Errorf("name field: want Protected, got %v", def.Visibility)
		}
		if def.Name == "createdAt" && def.Visibility != types.VisibilityPublic {
			t.Errorf("createdAt field: want Public, got %v", def.Visibility)
		}
		if def.Name == "balance" && def.Visibility != types.VisibilityInternal {
			t.Errorf("balance field: want Internal, got %v", def.Visibility)
		}
	}
}

func TestCSharpExtractor_Calls(t *testing.T) {
	source := []byte(`
public class Service
{
    public void Process()
    {
        var result = GetData();
        var item = items.First();
        logger.Log("message");
        Console.WriteLine("test");
    }
}
`)

	e := &CSharpExtractor{}
	output, err := e.Extract(source, "test.cs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	callNames := make(map[string]bool)
	for _, call := range output.Calls {
		callNames[call.Name] = true
	}

	// Check for method calls
	if len(output.Calls) == 0 {
		t.Error("Expected to find method calls")
	}
}