package extractors

import (
	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// CSharpExtractor extracts symbols from C# source code.
type CSharpExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from C# source code.
func (e *CSharpExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.CSharpLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from C# AST.
func (e *CSharpExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	switch node.Type(lang) {
	case "method_declaration":
		e.extractMethod(node, source, lang, output)
	case "class_declaration":
		e.extractClass(node, source, lang, output)
	case "interface_declaration":
		e.extractInterface(node, source, lang, output)
	case "enum_declaration":
		e.extractEnum(node, source, lang, output)
	case "struct_declaration":
		e.extractStruct(node, source, lang, output)
	case "field_declaration":
		e.extractField(node, source, lang, output)
	case "using_directive":
		e.extractImport(node, source, lang, output)
	case "invocation_expression":
		e.extractCall(node, source, lang, output)
	}

	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}
}

func (e *CSharpExtractor) extractMethod(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   isPublicCSharp(node, source, lang),
		Visibility: getVisibilityCSharp(node, source, lang),
	}

	// Detect parent class/struct/interface by walking up the AST
	parent := node.Parent()
	for parent != nil {
		pt := parent.Type(lang)
		if pt == "class_declaration" {
			pName := getChildByFieldNameContent(parent, "name", source, lang)
			if pName != "" {
				def.Parent = pName
				def.ParentKind = types.SymbolKindClass
			}
			break
		}
		if pt == "struct_declaration" {
			pName := getChildByFieldNameContent(parent, "name", source, lang)
			if pName != "" {
				def.Parent = pName
				def.ParentKind = types.SymbolKindStruct
			}
			break
		}
		if pt == "interface_declaration" {
			pName := getChildByFieldNameContent(parent, "name", source, lang)
			if pName != "" {
				def.Parent = pName
				def.ParentKind = types.SymbolKindInterface
			}
			break
		}
		parent = parent.Parent()
	}

	output.Definitions = append(output.Definitions, def)
}

func (e *CSharpExtractor) extractClass(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   isPublicCSharp(node, source, lang),
		Visibility: getVisibilityCSharp(node, source, lang),
	}
	output.Definitions = append(output.Definitions, def)

	// Extract base class and interfaces from base_list
	classRelation := types.ClassRelation{
		ClassName: name,
		Line:      int(startPoint.Row) + 1,
	}
	firstBaseFound := false

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "base_list" {
			for j := 0; j < child.ChildCount(); j++ {
				baseChild := child.Child(j)
				if baseChild == nil {
					continue
				}
				bt := baseChild.Type(lang)
				if bt == "identifier" || bt == "qualified_name" || bt == "generic_name" {
					baseName := baseChild.Text(source)
					if !firstBaseFound {
						classRelation.Extends = baseName
						firstBaseFound = true
					} else {
						classRelation.Implements = append(classRelation.Implements, baseName)
					}
				}
			}
		}
	}

	if classRelation.Extends != "" || len(classRelation.Implements) > 0 {
		output.Classes = append(output.Classes, classRelation)
	}
}

func (e *CSharpExtractor) extractInterface(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   isPublicCSharp(node, source, lang),
		Visibility: getVisibilityCSharp(node, source, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *CSharpExtractor) extractEnum(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   isPublicCSharp(node, source, lang),
		Visibility: getVisibilityCSharp(node, source, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *CSharpExtractor) extractStruct(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   isPublicCSharp(node, source, lang),
		Visibility: getVisibilityCSharp(node, source, lang),
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *CSharpExtractor) extractField(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "variable_declaration" {
			for j := 0; j < child.ChildCount(); j++ {
				declarator := child.Child(j)
				if declarator.Type(lang) == "variable_declarator" {
					name := getChildByFieldNameContent(declarator, "name", source, lang)
					if name != "" {
						startPoint := node.StartPoint()
						def := types.Definition{
							Name:       name,
							Kind:       types.SymbolKindField,
							Line:       int(startPoint.Row) + 1,
							Exported:   isPublicCSharp(node, source, lang),
							Visibility: getVisibilityCSharp(node, source, lang),
						}
						output.Definitions = append(output.Definitions, def)
					}
				}
			}
		}
	}
}

func (e *CSharpExtractor) extractImport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "qualified_name" || child.Type(lang) == "identifier" {
			imp := types.Import{
				Source: child.Text(source),
				Line:   int(startPoint.Row) + 1,
			}
			output.Imports = append(output.Imports, imp)
			break
		}
	}
}

func (e *CSharpExtractor) extractCall(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	expr := node.ChildByFieldName("function", lang)
	if expr == nil {
		return
	}

	call := types.Call{
		Line:   int(startPoint.Row) + 1,
		Column: int(startPoint.Column) + 1,
	}

	switch expr.Type(lang) {
	case "identifier":
		call.Name = expr.Text(source)
	case "member_access_expression":
		name := expr.ChildByFieldName("name", lang)
		obj := expr.ChildByFieldName("expression", lang)
		if name != nil {
			call.Name = name.Text(source)
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

func isPublicCSharp(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) bool {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "modifier" {
			// Modifier node's text is the modifier itself (e.g., "public", "private")
			modText := child.Text(source)
			if modText == "public" {
				return true
			}
		}
	}
	return false
}

func getVisibilityCSharp(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) types.Visibility {
	// Track all modifiers found
	hasPublic := false
	hasProtected := false
	hasPrivate := false
	hasInternal := false

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "modifier" {
			// Modifier node's text is the modifier itself
			modText := child.Text(source)
			switch modText {
			case "public":
				hasPublic = true
			case "protected":
				hasProtected = true
			case "private":
				hasPrivate = true
			case "internal":
				hasInternal = true
			}
		}
	}

	// Determine visibility based on modifiers
	// Priority: public > protected internal > internal > protected > private
	if hasPublic {
		return types.VisibilityPublic
	}
	if hasProtected && hasInternal {
		return types.VisibilityProtectedInternal
	}
	if hasProtected {
		return types.VisibilityProtected
	}
	if hasInternal {
		return types.VisibilityInternal
	}
	if hasPrivate {
		return types.VisibilityPrivate
	}

	// Default is private in C#
	return types.VisibilityPrivate
}