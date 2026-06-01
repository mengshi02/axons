package extractors

import (
	"testing"

	"github.com/mengshi02/axons/pkg/types"
)

func TestRustExtractor_Functions(t *testing.T) {
	source := []byte(`
pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

fn private_helper() -> bool {
    true
}
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	funcNames := definitionNames(output, types.SymbolKindFunction)
	assertContains(t, funcNames, "add")
	assertContains(t, funcNames, "private_helper")

	for _, def := range output.Definitions {
		switch def.Name {
		case "add":
			if def.Visibility != types.VisibilityPublic || !def.Exported {
				t.Errorf("add: want Public+Exported, got vis=%v exported=%v", def.Visibility, def.Exported)
			}
		case "private_helper":
			if def.Visibility != types.VisibilityPrivate || def.Exported {
				t.Errorf("private_helper: want Private+not Exported, got vis=%v exported=%v", def.Visibility, def.Exported)
			}
		}
	}
}

func TestRustExtractor_Struct(t *testing.T) {
	source := []byte(`
pub struct Point {
    pub x: f64,
    pub y: f64,
}

struct Internal {
    value: i32,
}
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	structNames := definitionNames(output, types.SymbolKindStruct)
	assertContains(t, structNames, "Point")
	assertContains(t, structNames, "Internal")

	for _, def := range output.Definitions {
		if def.Name == "Point" && def.Kind == types.SymbolKindStruct {
			if !def.Exported {
				t.Errorf("Point: want Exported=true")
			}
		}
		if def.Name == "Internal" && def.Kind == types.SymbolKindStruct {
			if def.Exported {
				t.Errorf("Internal: want Exported=false")
			}
		}
	}
}

func TestRustExtractor_Enum(t *testing.T) {
	source := []byte(`
pub enum Direction {
    North,
    South,
    East,
    West,
}

enum Hidden { A, B }
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	enumNames := definitionNames(output, types.SymbolKindEnum)
	assertContains(t, enumNames, "Direction")
	assertContains(t, enumNames, "Hidden")
}

func TestRustExtractor_Trait(t *testing.T) {
	source := []byte(`
pub trait Animal {
    fn speak(&self) -> String;
    fn name(&self) -> &str;
}

trait Private {
    fn hidden(&self);
}
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	traitNames := definitionNames(output, types.SymbolKindTrait)
	assertContains(t, traitNames, "Animal")
	assertContains(t, traitNames, "Private")
}

func TestRustExtractor_Impl(t *testing.T) {
	source := []byte(`
pub struct Dog;

pub trait Animal {
    fn speak(&self);
}

impl Animal for Dog {
    fn speak(&self) {
        println!("woof");
    }
}

impl Dog {
    pub fn new() -> Self { Dog }
}
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// impl Animal for Dog should produce a ClassRelation
	found := false
	for _, rel := range output.Classes {
		if rel.ClassName == "Dog" && len(rel.Implements) > 0 && rel.Implements[0] == "Animal" {
			found = true
		}
	}
	if !found {
		t.Errorf("Expected Dog implements Animal, got: %+v", output.Classes)
	}
}

func TestRustExtractor_UseSimple(t *testing.T) {
	source := []byte(`
use std::collections::HashMap;
use crate::utils;
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	srcs := importSources(output)
	assertContains(t, srcs, "std::collections::HashMap")
	assertContains(t, srcs, "crate::utils")
}

func TestRustExtractor_UseList(t *testing.T) {
	// Bug fix test: use std::io::{Read, Write} -> scoped_use_list
	source := []byte(`
use std::io::{Read, Write};
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(output.Imports) != 2 {
		t.Errorf("Expected 2 imports from use list, got %d: %+v", len(output.Imports), output.Imports)
	}
	srcs := importSources(output)
	assertContains(t, srcs, "std::io::Read")
	assertContains(t, srcs, "std::io::Write")
}

func TestRustExtractor_FunctionCalls(t *testing.T) {
	source := []byte(`
fn main() {
    let v = vec![1, 2, 3];
    let s = String::from("hello");
    v.push(4);
}
`)
	e := &RustExtractor{}
	output, err := e.Extract(source, "test.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	names := callNameList(output)
	assertContains(t, names, "push")
}

func TestRustExtractor_EmptySource(t *testing.T) {
	e := &RustExtractor{}
	output, err := e.Extract([]byte(""), "empty.rs")
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if len(output.Definitions) != 0 {
		t.Errorf("Expected 0 definitions, got %d", len(output.Definitions))
	}
}