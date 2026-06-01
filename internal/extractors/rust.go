package extractors

import (
	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// RustExtractor extracts symbols from Rust source code.
type RustExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from Rust source code.
func (e *RustExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.RustLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from Rust AST.
func (e *RustExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	switch node.Type(lang) {
	case "function_item":
		e.extractFunction(node, source, lang, output)
	case "struct_item":
		e.extractStruct(node, source, lang, output)
	case "enum_item":
		e.extractEnum(node, source, lang, output)
	case "trait_item":
		e.extractTrait(node, source, lang, output)
	case "impl_item":
		e.extractImpl(node, source, lang, output)
	case "use_declaration":
		e.extractImport(node, source, lang, output)
	case "call_expression":
		e.extractCall(node, source, lang, output)
	}

	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}
}

func (e *RustExtractor) extractFunction(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindFunction,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicRust(node, lang),
		Visibility: getVisibilityRust(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *RustExtractor) extractStruct(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindStruct,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicRust(node, lang),
		Visibility: getVisibilityRust(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *RustExtractor) extractEnum(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindEnum,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicRust(node, lang),
		Visibility: getVisibilityRust(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *RustExtractor) extractTrait(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindTrait,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicRust(node, lang),
		Visibility: getVisibilityRust(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *RustExtractor) extractImpl(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	typeNode := node.ChildByFieldName("type", lang)
	if typeNode == nil {
		return
	}

	typeName := typeNode.Text(source)
	startPoint := node.StartPoint()

	traitNode := node.ChildByFieldName("trait", lang)
	if traitNode != nil {
		rel := types.ClassRelation{
			ClassName:  typeName,
			Implements: []string{traitNode.Text(source)},
			Line:       int(startPoint.Row) + 1,
		}
		output.Classes = append(output.Classes, rel)
	}

	// Extract methods inside impl block and set Parent for CONTAINS edges
	// Iterate over the body of the impl_item to find function_item children
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		// Look for the declaration_list (body) of the impl
		if child.Type(lang) == "declaration_list" {
			for j := 0; j < child.ChildCount(); j++ {
				fn := child.Child(j)
				if fn == nil {
					continue
				}
				if fn.Type(lang) == "function_item" {
					fnName := getChildByFieldNameContent(fn, "name", source, lang)
					if fnName == "" {
						continue
					}
					fnStart := fn.StartPoint()
					fnEnd := fn.EndPoint()
					def := types.Definition{
						Name:       fnName,
						Kind:       types.SymbolKindMethod,
						Line:       int(fnStart.Row) + 1,
						EndLine:    int(fnEnd.Row) + 1,
						Exported:   isPublicRust(fn, lang),
						Visibility: getVisibilityRust(fn, lang),
						Parent:     typeName,
						ParentKind: types.SymbolKindStruct,
					}
					output.Definitions = append(output.Definitions, def)
				}
			}
		}
	}
}

func (e *RustExtractor) extractImport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "scoped_identifier", "identifier":
			// e.g. use std::collections::HashMap;
			imp := types.Import{
				Source: child.Text(source),
				Line:   int(startPoint.Row) + 1,
			}
			output.Imports = append(output.Imports, imp)
		case "scoped_use_list":
			// Bug fix: e.g. use std::io::{Read, Write};
			// scoped_use_list = prefix_path + "::" + use_list
			prefix := ""
			for j := 0; j < child.ChildCount(); j++ {
				part := child.Child(j)
				if part == nil {
					continue
				}
				pt := part.Type(lang)
				if pt == "scoped_identifier" || pt == "identifier" {
					prefix = part.Text(source)
				} else if pt == "use_list" {
					for k := 0; k < part.ChildCount(); k++ {
						spec := part.Child(k)
						if spec == nil {
							continue
						}
						st := spec.Type(lang)
						if st == "identifier" || st == "scoped_identifier" {
							source_path := spec.Text(source)
							if prefix != "" {
								source_path = prefix + "::" + source_path
							}
							imp := types.Import{
								Source:  source_path,
								Line:    int(startPoint.Row) + 1,
								IsNamed: true,
							}
							output.Imports = append(output.Imports, imp)
						}
					}
				}
			}
		case "use_list":
			// standalone use_list without prefix (rare but possible)
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec == nil {
					continue
				}
				if spec.Type(lang) == "identifier" || spec.Type(lang) == "scoped_identifier" {
					imp := types.Import{
						Source:  spec.Text(source),
						Line:    int(startPoint.Row) + 1,
						IsNamed: true,
					}
					output.Imports = append(output.Imports, imp)
				}
			}
		}
	}
}

func (e *RustExtractor) extractCall(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	funcNode := node.ChildByFieldName("function", lang)
	if funcNode == nil {
		return
	}

	call := types.Call{
		Line:   int(startPoint.Row) + 1,
		Column: int(startPoint.Column) + 1,
	}

	switch funcNode.Type(lang) {
	case "identifier":
		call.Name = funcNode.Text(source)
	case "scoped_identifier":
		call.Name = funcNode.Text(source)
	case "field_expression":
		field := funcNode.ChildByFieldName("field", lang)
		obj := funcNode.ChildByFieldName("value", lang)
		if field != nil {
			call.Name = field.Text(source)
			call.IsMethod = true
			if obj != nil {
				call.Receiver = obj.Text(source)
			}
		}
	default:
		return
	}

	if call.Name != "" {
		output.Calls = append(output.Calls, call)
	}
}

func isPublicRust(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "visibility_modifier" {
			return true
		}
	}
	return false
}

func getVisibilityRust(node *gotreesitter.Node, lang *gotreesitter.Language) types.Visibility {
	if isPublicRust(node, lang) {
		return types.VisibilityPublic
	}
	return types.VisibilityPrivate
}