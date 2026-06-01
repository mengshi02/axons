package extractors

import (
	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TypeScriptExtractor extracts symbols from TypeScript source code.
type TypeScriptExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from TypeScript source code.
func (e *TypeScriptExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.TypescriptLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from TypeScript AST.
func (e *TypeScriptExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	nodeType := node.Type(lang)

	// Handle TypeScript-specific node types
	switch nodeType {
	case "interface_declaration":
		e.extractInterface(node, source, lang, output)
	case "type_alias_declaration":
		e.extractTypeAlias(node, source, lang, output)
	case "enum_declaration":
		e.extractEnum(node, source, lang, output)
	case "class_declaration":
		e.extractClass(node, source, lang, output)
	case "abstract_class_declaration":
		e.extractClass(node, source, lang, output)
	case "module":
		e.extractModule(node, source, lang, output)
	case "import_statement":
		e.extractImport(node, source, lang, output)
	case "import_type_statement":
		// TypeScript type-only import: import type { Foo } from 'bar'
		e.extractImportType(node, source, lang, output)
	}

	// Always recurse into children for all node types
	// This ensures we find nested definitions
	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}

	// Also handle export_statement specially to extract exported items
	if nodeType == "export_statement" {
		e.handleExportStatement(node, source, lang, output)
	}
}

// handleExportStatement extracts definitions from export statements
// Note: This is no longer needed since we recurse into children,
// but kept for potential future use.
func (e *TypeScriptExtractor) handleExportStatement(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	// Export statements can contain type declarations
	// This is handled by the recursive traversal now
}

func (e *TypeScriptExtractor) extractInterface(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindInterface,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)

	// Extract extends relationships (interfaces can extend other interfaces)
	classRelation := types.ClassRelation{
		ClassName: name,
		Line:      int(startPoint.Row) + 1,
	}

	// Look for extends clause in interface (TypeScript uses "extends_type_clause")
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		childType := child.Type(lang)
		if childType == "extends_clause" || childType == "extends_type_clause" {
			for j := 0; j < child.ChildCount(); j++ {
				subChild := child.Child(j)
				if subChild == nil {
					continue
				}
				subType := subChild.Type(lang)
				// Skip "extends" keyword and punctuation
				if subType == "extends" || subType == "," {
					continue
				}
				// Get the parent interface name
				if subType == "type_identifier" || subType == "identifier" {
					classRelation.Implements = append(classRelation.Implements, subChild.Text(source))
				}
			}
		}
	}

	// Add as extends relationship (interfaces extend other interfaces)
	if len(classRelation.Implements) > 0 {
		// For interfaces, we use Extends to store parent interfaces
		// since interfaces "extend" other interfaces, not "implement"
		if len(classRelation.Implements) == 1 {
			classRelation.Extends = classRelation.Implements[0]
			classRelation.Implements = nil
		}
		output.Classes = append(output.Classes, classRelation)
	}
}

func (e *TypeScriptExtractor) extractTypeAlias(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindType,
		Line:       int(startPoint.Row) + 1,
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *TypeScriptExtractor) extractEnum(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *TypeScriptExtractor) extractModule(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindModule,
		Line:       int(startPoint.Row) + 1,
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *TypeScriptExtractor) extractClass(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	// Extract decorators
	decorators := e.extractDecorators(node, source, lang)

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindClass,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isExportedJS(node, lang),
		Visibility: getVisibilityJS(node, lang),
		Decorators: decorators,
	}
	output.Definitions = append(output.Definitions, def)

	// Extract extends and implements relationships
	classRelation := types.ClassRelation{
		ClassName: name,
		Line:      int(startPoint.Row) + 1,
	}

	// Look for extends and implements clauses
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		childType := child.Type(lang)

		// Handle class_heritage (contains extends_clause and/or implements_clause)
		if childType == "class_heritage" {
			for j := 0; j < child.ChildCount(); j++ {
				heritageChild := child.Child(j)
				if heritageChild == nil {
					continue
				}
				heritageType := heritageChild.Type(lang)

				// Handle extends_clause inside class_heritage
				if heritageType == "extends_clause" {
					for k := 0; k < heritageChild.ChildCount(); k++ {
						subChild := heritageChild.Child(k)
						if subChild == nil {
							continue
						}
						subType := subChild.Type(lang)
						// Skip "extends" keyword
						if subType == "extends" {
							continue
						}
						// Get the parent class name
						if subType == "type_identifier" || subType == "identifier" || subType == "member_expression" {
							classRelation.Extends = subChild.Text(source)
							break
						}
					}
				}

				// Handle implements_clause inside class_heritage
				if heritageType == "implements_clause" {
					for k := 0; k < heritageChild.ChildCount(); k++ {
						subChild := heritageChild.Child(k)
						if subChild == nil {
							continue
						}
						subType := subChild.Type(lang)
						// Skip "implements" keyword
						if subType == "implements" {
							continue
						}
						// Get interface name
						if subType == "type_identifier" || subType == "identifier" {
							classRelation.Implements = append(classRelation.Implements, subChild.Text(source))
						}
						// Handle generic types like SomeInterface<T>
						if subType == "generic_type" {
							for m := 0; m < subChild.ChildCount(); m++ {
								genericChild := subChild.Child(m)
								if genericChild != nil && (genericChild.Type(lang) == "type_identifier" || genericChild.Type(lang) == "identifier") {
									classRelation.Implements = append(classRelation.Implements, genericChild.Text(source))
									break
								}
							}
						}
					}
				}
			}
		}

		// Handle extends_clause directly (JavaScript style)
		if childType == "extends_clause" {
			for j := 0; j < child.ChildCount(); j++ {
				subChild := child.Child(j)
				if subChild == nil {
					continue
				}
				subType := subChild.Type(lang)
				// Skip "extends" keyword
				if subType == "extends" {
					continue
				}
				// Get the parent class name
				if subType == "type_identifier" || subType == "identifier" || subType == "member_expression" {
					classRelation.Extends = subChild.Text(source)
					break
				}
			}
		}

		// Handle implements clause directly (when not inside class_heritage)
		if childType == "implements_clause" {
			for j := 0; j < child.ChildCount(); j++ {
				subChild := child.Child(j)
				if subChild == nil {
					continue
				}
				subType := subChild.Type(lang)
				// Skip "implements" keyword
				if subType == "implements" {
					continue
				}
				// Get interface name
				if subType == "type_identifier" || subType == "identifier" {
					classRelation.Implements = append(classRelation.Implements, subChild.Text(source))
				}
				// Handle generic types like SomeInterface<T>
				if subType == "generic_type" {
					// Get the first child which is the type name
					for k := 0; k < subChild.ChildCount(); k++ {
						genericChild := subChild.Child(k)
						if genericChild != nil && (genericChild.Type(lang) == "type_identifier" || genericChild.Type(lang) == "identifier") {
							classRelation.Implements = append(classRelation.Implements, genericChild.Text(source))
							break
						}
					}
				}
			}
		}
	}

	// Only add if we found inheritance relationships
	if classRelation.Extends != "" || len(classRelation.Implements) > 0 {
		output.Classes = append(output.Classes, classRelation)
	}
}

// extractImport extracts import statements (TypeScript has same syntax as JavaScript).
func (e *TypeScriptExtractor) extractImport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	js := JavaScriptExtractor{}
	js.extractImport(node, source, lang, output)
}

// extractImportType extracts TypeScript type-only import statements.
// Syntax: import type { Foo } from 'bar'
func (e *TypeScriptExtractor) extractImportType(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		IsType: true, // Mark as type-only import
	}

	// Extract imported type names
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		// Handle type import specifier
		if child.Type(lang) == "type_import_clause" || child.Type(lang) == "named_imports" {
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec == nil {
					continue
				}
				if spec.Type(lang) == "import_specifier" || spec.Type(lang) == "type_identifier" {
					name := spec.Text(source)
					if name != "" {
						imp.Symbols = append(imp.Symbols, name)
					}
				}
			}
		}
	}

	output.Imports = append(output.Imports, imp)
}

// extractDecorators extracts TypeScript decorators from a node.
func (e *TypeScriptExtractor) extractDecorators(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) []types.Decorator {
	var decorators []types.Decorator
	
	// In TypeScript, decorators appear as "decorator" nodes before the declaration
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		
		if child.Type(lang) == "decorator" {
			decorator := e.parseDecorator(child, source, lang)
			if decorator.Name != "" {
				decorators = append(decorators, decorator)
			}
		}
	}
	
	return decorators
}

// parseDecorator parses a single decorator node.
func (e *TypeScriptExtractor) parseDecorator(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) types.Decorator {
	startPoint := node.StartPoint()
	
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		
		childType := child.Type(lang)
		
		switch childType {
		case "identifier":
			// Simple decorator: @Injectable
			return types.Decorator{
				Name: child.Text(source),
				Line: int(startPoint.Row) + 1,
			}
		
		case "member_expression":
			// Member decorator: @Namespace.Decorator
			return types.Decorator{
				Name: child.Text(source),
				Line: int(startPoint.Row) + 1,
			}
		
		case "call_expression":
			// Call decorator: @Component({...}) or @Inject()
			decorator := types.Decorator{
				Line: int(startPoint.Row) + 1,
			}
			
			// Get the function name
			funcNode := child.ChildByFieldName("function", lang)
			if funcNode != nil {
				decorator.Name = funcNode.Text(source)
			}
			
			// Get arguments
			argsNode := child.ChildByFieldName("arguments", lang)
			if argsNode != nil {
				decorator.Args = argsNode.Text(source)
			}
			
			return decorator
		}
	}
	
	return types.Decorator{}
}

// TSXExtractor extracts symbols from TSX source code.
type TSXExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from TSX source code.
func (e *TSXExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	ts := TypeScriptExtractor{}
	return extractWithLanguage(source, grammars.TsxLanguage(), ts.extractNodes)
}