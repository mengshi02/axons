package extractors

import (
	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// JavaScriptExtractor extracts symbols from JavaScript source code.
type JavaScriptExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from JavaScript source code.
func (e *JavaScriptExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.JavascriptLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from JavaScript AST.
func (e *JavaScriptExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	switch node.Type(lang) {
	case "function_declaration":
		e.extractFunction(node, source, lang, output)
	case "function_expression":
		e.extractFunctionExpression(node, source, lang, output)
	case "arrow_function":
		// Arrow functions don't have names
	case "method_definition":
		e.extractMethod(node, source, lang, output)
	case "class_declaration":
		e.extractClass(node, source, lang, output)
	case "variable_declaration":
		e.extractVariable(node, source, lang, output)
	case "lexical_declaration":
		e.extractVariable(node, source, lang, output)
	case "import_statement":
		e.extractImport(node, source, lang, output)
	case "export_statement":
		e.extractExport(node, source, lang, output)
	case "call_expression":
		e.extractCall(node, source, lang, output)
	}

	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}
}

func (e *JavaScriptExtractor) extractFunction(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *JavaScriptExtractor) extractFunctionExpression(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindFunction,
		Line:       int(startPoint.Row) + 1,
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *JavaScriptExtractor) extractMethod(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindMethod,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *JavaScriptExtractor) extractClass(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindClass,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)

	// Extract class body (methods, properties)
	body := node.ChildByFieldName("body", lang)
	if body != nil {
		e.extractClassBody(body, source, lang, output, name)
	}

	// Extract extends (inheritance)
	classRelation := types.ClassRelation{
		ClassName: name,
		Line:      int(startPoint.Row) + 1,
	}

	// Look for extends clause - it's a child with type "extends_clause" or through parent field
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "extends_clause" || child.Type(lang) == "class_heritage" {
			// Get the parent class name
			for j := 0; j < child.ChildCount(); j++ {
				subChild := child.Child(j)
				if subChild == nil {
					continue
				}
				// Skip "extends" keyword
				if subChild.Type(lang) == "extends" {
					continue
				}
				// Get the parent class identifier
				if subChild.Type(lang) == "identifier" || subChild.Type(lang) == "member_expression" {
					classRelation.Extends = subChild.Text(source)
					break
				}
				// Handle member_expression (e.g., extends some.Module)
				if subChild.Type(lang) == "member_expression" {
					classRelation.Extends = subChild.Text(source)
					break
				}
			}
		}
	}

	// Only add if we found an extends relationship
	if classRelation.Extends != "" {
		output.Classes = append(output.Classes, classRelation)
	}
}

func (e *JavaScriptExtractor) extractClassBody(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput, className string) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "method_definition":
			name := getChildByFieldNameContent(child, "name", source, lang)
			if name != "" {
				startPoint := child.StartPoint()
				def := types.Definition{
					Name:       name,
					Kind:       types.SymbolKindMethod,
					Line:       int(startPoint.Row) + 1,
					Parent:     className,
					ParentKind: types.SymbolKindClass,
					Exported:   isExportedJS(child, lang),
					Visibility: getVisibilityJS(child, lang),
				}
				output.Definitions = append(output.Definitions, def)
			}
		case "field_definition":
			name := getChildByFieldNameContent(child, "name", source, lang)
			if name != "" {
				startPoint := child.StartPoint()
				def := types.Definition{
					Name:       name,
					Kind:       types.SymbolKindProperty,
					Line:       int(startPoint.Row) + 1,
					Parent:     className,
					ParentKind: types.SymbolKindClass,
					Exported:   isExportedJS(child, lang),
					Visibility: getVisibilityJS(child, lang),
				}
				output.Definitions = append(output.Definitions, def)
			}
		}
	}
}

func (e *JavaScriptExtractor) extractVariable(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "variable_declarator" {
			name := getChildByFieldNameContent(child, "name", source, lang)
			if name == "" {
				continue
			}

			startPoint := child.StartPoint()

			// Check if value is a function/arrow function
			value := child.ChildByFieldName("value", lang)
			kind := types.SymbolKindVariable
			if value != nil {
				switch value.Type(lang) {
				case "function_expression", "arrow_function":
					kind = types.SymbolKindFunction
				}
			}

			def := types.Definition{
				Name:       name,
				Kind:       kind,
				Line:       int(startPoint.Row) + 1,
				Exported:   isExportedJS(node, lang),
				Visibility: getVisibilityJS(node, lang),
			}
			output.Definitions = append(output.Definitions, def)
		}
	}
}

func (e *JavaScriptExtractor) extractImport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	// Get the source (module path)
	sourcePath := ""
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "string" {
			content := child.Text(source)
			if len(content) >= 2 {
				sourcePath = content[1 : len(content)-1]
			}
			break
		}
	}

	if sourcePath == "" {
		return
	}

	imp := types.Import{
		Source: sourcePath,
		Line:   int(startPoint.Row) + 1,
	}

	// Check for import clause
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "import_clause":
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec == nil {
					continue
				}
				switch spec.Type(lang) {
				case "identifier":
					imp.IsDefault = true
					imp.Alias = spec.Text(source)
					imp.Symbols = append(imp.Symbols, imp.Alias)
				case "namespace_import":
					for k := 0; k < spec.ChildCount(); k++ {
						childK := spec.Child(k)
						if childK != nil && childK.Type(lang) == "identifier" {
							imp.Alias = childK.Text(source)
							// For namespace import like import * as foo, we don't have specific symbols
						}
					}
				case "named_imports":
					imp.IsNamed = true
					// Extract individual import specifiers
					for k := 0; k < spec.ChildCount(); k++ {
						importSpec := spec.Child(k)
						if importSpec == nil {
							continue
						}
						if importSpec.Type(lang) == "import_specifier" {
							// Get the name (and possibly alias)
							name := getChildByFieldNameContent(importSpec, "name", source, lang)
							if name != "" {
								imp.Symbols = append(imp.Symbols, name)
							}
						}
					}
				}
			}
		}
	}

	output.Imports = append(output.Imports, imp)
}

func (e *JavaScriptExtractor) extractExport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "export_clause":
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec == nil {
					continue
				}
				if spec.Type(lang) == "export_specifier" {
					name := getChildByFieldNameContent(spec, "name", source, lang)
					if name != "" {
						exp := types.Export{
							Name: name,
							Line: int(startPoint.Row) + 1,
						}
						output.Exports = append(output.Exports, exp)
					}
				}
			}
		case "function_declaration", "class_declaration":
			name := getChildByFieldNameContent(child, "name", source, lang)
			if name != "" {
				exp := types.Export{
					Name: name,
					Line: int(startPoint.Row) + 1,
				}
				output.Exports = append(output.Exports, exp)
			}
		case "lexical_declaration", "variable_declaration":
			// Handle: export const x = 1; export var y = 2;
			for j := 0; j < child.ChildCount(); j++ {
				declarator := child.Child(j)
				if declarator == nil {
					continue
				}
				if declarator.Type(lang) == "variable_declarator" {
					name := getChildByFieldNameContent(declarator, "name", source, lang)
					if name != "" {
						exp := types.Export{
							Name: name,
							Line: int(startPoint.Row) + 1,
						}
						output.Exports = append(output.Exports, exp)
					}
				}
			}
		}
	}
}

func (e *JavaScriptExtractor) extractCall(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
	case "member_expression":
		prop := funcNode.ChildByFieldName("property", lang)
		obj := funcNode.ChildByFieldName("object", lang)
		if prop != nil {
			call.Name = prop.Text(source)
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

func isExportedJS(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	parent := node.Parent()
	for parent != nil {
		if parent.Type(lang) == "export_statement" {
			return true
		}
		parent = parent.Parent()
	}
	return false
}

func getVisibilityJS(node *gotreesitter.Node, lang *gotreesitter.Language) types.Visibility {
	if isExportedJS(node, lang) {
		return types.VisibilityPublic
	}
	return types.VisibilityPrivate
}