package extractors

import (
	"strings"

	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// GoExtractor extracts symbols from Go source code.
type GoExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from Go source code.
func (e *GoExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.GoLanguage(), e.extractNodes)
}

// extractNodes recursively extracts nodes from the AST.
func (e *GoExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	switch node.Type(lang) {
	case "function_declaration":
		e.extractFunction(node, source, lang, output)
	case "method_declaration":
		e.extractMethod(node, source, lang, output)
	case "type_declaration":
		e.extractTypeDeclaration(node, source, lang, output)
	case "import_declaration":
		e.extractImport(node, source, lang, output)
	case "call_expression":
		e.extractCall(node, source, lang, output)
	}

	// Recursively process children
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		e.extractNodes(child, source, lang, output)
	}
}

// extractFunction extracts a function definition.
func (e *GoExtractor) extractFunction(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	// Extract documentation comment
	doc := e.extractDocumentation(node, source, lang)

	def := types.Definition{
		Name:          name,
		Kind:          types.SymbolKindFunction,
		Line:          int(startPoint.Row) + 1,
		EndLine:       int(endPoint.Row) + 1,
		Exported:      isExportedGo(name),
		Visibility:    getVisibilityGo(name),
		Documentation: doc,
	}

	output.Definitions = append(output.Definitions, def)
}

// extractMethod extracts a method definition.
func (e *GoExtractor) extractMethod(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	receiver := e.getReceiver(node, source, lang)
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	doc := e.extractDocumentation(node, source, lang)

	def := types.Definition{
		Name:          name,
		Kind:          types.SymbolKindMethod,
		Line:          int(startPoint.Row) + 1,
		EndLine:       int(endPoint.Row) + 1,
		Exported:      isExportedGo(name),
		Visibility:    getVisibilityGo(name),
		Parent:        receiver,
		ParentKind:    types.SymbolKindStruct,
		Documentation: doc,
	}

	output.Definitions = append(output.Definitions, def)
}

// extractTypeDeclaration extracts type declarations (structs, interfaces, etc.).
func (e *GoExtractor) extractTypeDeclaration(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	// Get the type spec
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "type_spec" {
			name := getChildByFieldNameContent(child, "name", source, lang)
			if name == "" {
				continue
			}

			startPoint := child.StartPoint()
			endPoint := child.EndPoint()
			
			// Extract documentation from the type declaration node
			doc := e.extractDocumentation(node, source, lang)

			// Determine if it's a struct, interface, or other type
			kind := types.SymbolKindType
			typeNode := child.ChildByFieldName("type", lang)
			if typeNode != nil {
				switch typeNode.Type(lang) {
				case "struct_type":
					kind = types.SymbolKindStruct
				case "interface_type":
					kind = types.SymbolKindInterface
				}
			}

			def := types.Definition{
				Name:          name,
				Kind:          kind,
				Line:          int(startPoint.Row) + 1,
				EndLine:       int(endPoint.Row) + 1,
				Exported:      isExportedGo(name),
				Visibility:    getVisibilityGo(name),
				Documentation: doc,
			}

			output.Definitions = append(output.Definitions, def)

			// Extract struct fields and embedded types
			if kind == types.SymbolKindStruct && typeNode != nil {
				e.extractStructFields(typeNode, source, lang, output, name)
			}

			// Extract interface embedded types (interface inheritance)
			if kind == types.SymbolKindInterface && typeNode != nil {
				e.extractInterfaceEmbeds(typeNode, source, lang, output, name)
			}
		}
	}
}

// extractStructFields extracts fields from a struct type and detects embedded types.
func (e *GoExtractor) extractStructFields(structNode *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput, structName string) {
	// Find field_declaration_list
	for i := 0; i < structNode.ChildCount(); i++ {
		child := structNode.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "field_declaration_list" {
			for j := 0; j < child.ChildCount(); j++ {
				field := child.Child(j)
				if field == nil {
					continue
				}
				if field.Type(lang) == "field_declaration" {
					// Check if this is an embedded field (no field name, just type)
					// Embedded fields have no field_identifier, only a type
					hasFieldName := false
					var embeddedType string
					startPoint := field.StartPoint()

					for k := 0; k < field.ChildCount(); k++ {
						fchild := field.Child(k)
						if fchild == nil {
							continue
						}
						fchildType := fchild.Type(lang)

						if fchildType == "field_identifier" {
							hasFieldName = true
							name := fchild.Text(source)
							startPoint = fchild.StartPoint()

							def := types.Definition{
								Name:       name,
								Kind:       types.SymbolKindField,
								Line:       int(startPoint.Row) + 1,
								Exported:   isExportedGo(name),
								Visibility: getVisibilityGo(name),
								Parent:     structName,
								ParentKind: types.SymbolKindStruct,
							}
							output.Definitions = append(output.Definitions, def)
						}

						// Capture type for embedded fields
						if !hasFieldName && (fchildType == "type_identifier" || fchildType == "qualified_type" || fchildType == "pointer_type") {
							embeddedType = fchild.Text(source)
						}
					}

					// If no field name found, this is an embedded field (inheritance in Go)
					if !hasFieldName && embeddedType != "" {
						// Strip pointer prefix if present
						embeddedType = strings.TrimPrefix(embeddedType, "*")
						output.Classes = append(output.Classes, types.ClassRelation{
							ClassName: structName,
							Extends:   embeddedType,
							Line:      int(startPoint.Row) + 1,
						})
					}
				}
			}
		}
	}
}

// extractInterfaceEmbeds extracts embedded interfaces from an interface type.
// In Go tree-sitter, embedded interfaces appear as "type_elem" children of the interface_type node,
// not as children of an "interface_body" node.
func (e *GoExtractor) extractInterfaceEmbeds(interfaceNode *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput, interfaceName string) {
	// Iterate over direct children of the interface_type node
	for i := 0; i < interfaceNode.ChildCount(); i++ {
		child := interfaceNode.Child(i)
		if child == nil {
			continue
		}
		childType := child.Type(lang)

		// type_elem represents an embedded interface type
		if childType == "type_elem" {
			// The type_elem contains the actual type identifier
			for j := 0; j < child.ChildCount(); j++ {
				typeChild := child.Child(j)
				if typeChild == nil {
					continue
				}
				typeChildType := typeChild.Type(lang)

				// Handle simple type identifier (e.g., "Reader")
				if typeChildType == "type_identifier" {
					embeddedName := typeChild.Text(source)
					startPoint := typeChild.StartPoint()
					output.Classes = append(output.Classes, types.ClassRelation{
						ClassName: interfaceName,
						Extends:   embeddedName,
						Line:      int(startPoint.Row) + 1,
					})
				}
				// Handle qualified type (e.g., "io.Reader")
				if typeChildType == "qualified_type" {
					embeddedName := typeChild.Text(source)
					startPoint := typeChild.StartPoint()
					output.Classes = append(output.Classes, types.ClassRelation{
						ClassName: interfaceName,
						Extends:   embeddedName,
						Line:      int(startPoint.Row) + 1,
					})
				}
			}
		}

		// Also check for direct type_identifier (some tree-sitter versions might use this)
		if childType == "type_identifier" {
			embeddedName := child.Text(source)
			startPoint := child.StartPoint()
			output.Classes = append(output.Classes, types.ClassRelation{
				ClassName: interfaceName,
				Extends:   embeddedName,
				Line:      int(startPoint.Row) + 1,
			})
		}
	}
}

// extractImport extracts an import statement.
func (e *GoExtractor) extractImport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "import_spec":
			e.extractImportSpec(child, source, lang, output)
		case "import_spec_list":
			// Multiple imports
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec.Type(lang) == "import_spec" {
					e.extractImportSpec(spec, source, lang, output)
				}
			}
		}
	}
}

// extractImportSpec extracts a single import spec.
func (e *GoExtractor) extractImportSpec(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	// Get the path
	path := ""
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "interpreted_string_literal" {
			// Remove quotes
			content := child.Text(source)
			if len(content) >= 2 {
				path = content[1 : len(content)-1]
			}
			break
		}
	}

	if path == "" {
		return
	}

	// Check for alias
	alias := ""
	name := node.ChildByFieldName("name", lang)
	if name != nil {
		alias = name.Text(source)
	}

	imp := types.Import{
		Source:  path,
		Line:    int(startPoint.Row) + 1,
		Alias:   alias,
		IsNamed: alias != "" && alias != "_",
	}

	output.Imports = append(output.Imports, imp)
}

// extractCall extracts a function/method call.
func (e *GoExtractor) extractCall(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	// Get the function name
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
	case "selector_expression":
		// Method call: receiver.method
		operand := funcNode.ChildByFieldName("operand", lang)
		field := funcNode.ChildByFieldName("field", lang)
		if field != nil {
			call.Name = field.Text(source)
			call.IsMethod = true
			if operand != nil {
				call.Receiver = operand.Text(source)
			}
		}
	default:
		return
	}

	if call.Name != "" {
		output.Calls = append(output.Calls, call)
	}
}

// getReceiver gets the receiver of a method.
func (e *GoExtractor) getReceiver(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	params := node.ChildByFieldName("receiver", lang)
	if params == nil {
		return ""
	}

	// Look for type_identifier in the receiver
	for i := 0; i < params.ChildCount(); i++ {
		child := params.Child(i)
		if child.Type(lang) == "parameter_declaration" {
			for j := 0; j < child.ChildCount(); j++ {
				pchild := child.Child(j)
				if pchild.Type(lang) == "type_identifier" {
					return pchild.Text(source)
				}
				// Handle pointer types
				if pchild.Type(lang) == "pointer_type" {
					for k := 0; k < pchild.ChildCount(); k++ {
						tchild := pchild.Child(k)
						if tchild.Type(lang) == "type_identifier" {
							return tchild.Text(source)
						}
					}
				}
			}
		}
	}
	return ""
}

// isExportedGo checks if a name is exported in Go.
func isExportedGo(name string) bool {
	if len(name) == 0 {
		return false
	}
	// In Go, names starting with uppercase are exported
	return name[0] >= 'A' && name[0] <= 'Z'
}

// getVisibilityGo gets the visibility of a symbol.
func getVisibilityGo(name string) types.Visibility {
	if isExportedGo(name) {
		return types.VisibilityPublic
	}
	return types.VisibilityPrivate
}

// extractDocumentation extracts documentation comment for a node.
func (e *GoExtractor) extractDocumentation(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	// In Go tree-sitter, comments are stored as sibling nodes before the declaration
	// We need to look for preceding comment nodes
	
	parent := node.Parent()
	if parent == nil {
		return ""
	}
	
	// Find our index in parent's children
	myIndex := -1
	for i := 0; i < parent.ChildCount(); i++ {
		if parent.Child(i) == node {
			myIndex = i
			break
		}
	}
	if myIndex <= 0 {
		return ""
	}
	
	// Collect preceding comments
	var comments []string
	for i := myIndex - 1; i >= 0; i-- {
		sibling := parent.Child(i)
		if sibling == nil {
			continue
		}
		
		siblingType := sibling.Type(lang)
		
		// Check for comment nodes
		if siblingType == "comment" {
			commentText := sibling.Text(source)
			// Remove leading // or /* */
			commentText = strings.TrimSpace(commentText)
			if strings.HasPrefix(commentText, "//") {
				commentText = strings.TrimSpace(commentText[2:])
			} else if strings.HasPrefix(commentText, "/*") && strings.HasSuffix(commentText, "*/") {
				commentText = strings.TrimSpace(commentText[2 : len(commentText)-2])
			}
			// Prepend since we're going backwards
			comments = append([]string{commentText}, comments...)
		} else if siblingType != "\n" && siblingType != "newline" {
			// Stop if we hit a non-comment, non-newline node
			break
		}
	}
	
	return strings.Join(comments, "\n")
}