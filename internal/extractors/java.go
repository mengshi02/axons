package extractors

import (
	"strings"

	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// JavaExtractor extracts symbols from Java source code.
type JavaExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from Java source code.
func (e *JavaExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.JavaLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from Java AST.
func (e *JavaExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	switch node.Type(lang) {
	case "method_declaration":
		e.extractMethod(node, source, lang, output)
	case "constructor_declaration":
		e.extractConstructor(node, source, lang, output)
	case "class_declaration":
		e.extractClass(node, source, lang, output)
	case "interface_declaration":
		e.extractInterface(node, source, lang, output)
	case "enum_declaration":
		e.extractEnum(node, source, lang, output)
	case "field_declaration":
		e.extractField(node, source, lang, output)
	case "import_declaration":
		e.extractImport(node, source, lang, output)
	case "method_invocation":
		e.extractCall(node, source, lang, output)
	}

	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}
}

func (e *JavaExtractor) extractMethod(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	// Extract annotations
	annotations := e.extractAnnotations(node, source, lang)

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindMethod,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicJava(node, source, lang),
		Visibility: getVisibilityJava(node, source, lang),
		Decorators: annotations,
	}

	// Detect parent class/interface by walking up the AST
	parent := node.Parent()
	for parent != nil {
		pt := parent.Type(lang)
		if pt == "class_declaration" {
			className := getChildByFieldNameContent(parent, "name", source, lang)
			if className != "" {
				def.Parent = className
				def.ParentKind = types.SymbolKindClass
			}
			break
		}
		if pt == "interface_declaration" {
			ifaceName := getChildByFieldNameContent(parent, "name", source, lang)
			if ifaceName != "" {
				def.Parent = ifaceName
				def.ParentKind = types.SymbolKindInterface
			}
			break
		}
		parent = parent.Parent()
	}

	output.Definitions = append(output.Definitions, def)
}

// extractConstructor extracts constructor declarations.
func (e *JavaExtractor) extractConstructor(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	// Constructor name is directly an identifier child (no "name" field in Java grammar)
	name := ""
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "identifier" {
			name = child.Text(source)
			break
		}
	}
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	annotations := e.extractAnnotations(node, source, lang)

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindMethod,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicJava(node, source, lang),
		Visibility: getVisibilityJava(node, source, lang),
		Decorators: annotations,
	}

	// Detect parent class by walking up the AST
	parent := node.Parent()
	for parent != nil {
		if parent.Type(lang) == "class_declaration" {
			className := getChildByFieldNameContent(parent, "name", source, lang)
			if className != "" {
				def.Parent = className
				def.ParentKind = types.SymbolKindClass
			}
			break
		}
		parent = parent.Parent()
	}

	output.Definitions = append(output.Definitions, def)
}

func (e *JavaExtractor) extractClass(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	annotations := e.extractAnnotations(node, source, lang)

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindClass,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicJava(node, source, lang),
		Visibility: getVisibilityJava(node, source, lang),
		Decorators: annotations,
	}
	output.Definitions = append(output.Definitions, def)

	// Extract superclass (extends) and interfaces (implements)
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "superclass":
			// child has: "extends" keyword + type_identifier
			for j := 0; j < child.ChildCount(); j++ {
				tc := child.Child(j)
				if tc != nil && tc.Type(lang) == "type_identifier" {
					output.Classes = append(output.Classes, types.ClassRelation{
						ClassName: name,
						Extends:   tc.Text(source),
						Line:      int(startPoint.Row) + 1,
					})
				}
			}
		case "super_interfaces":
			// child has: "implements" keyword + type_list
			for j := 0; j < child.ChildCount(); j++ {
				tl := child.Child(j)
				if tl == nil || tl.Type(lang) != "type_list" {
					continue
				}
				for k := 0; k < tl.ChildCount(); k++ {
					tc := tl.Child(k)
					if tc != nil && tc.Type(lang) == "type_identifier" {
						rel := types.ClassRelation{
							ClassName:  name,
							Implements: []string{tc.Text(source)},
							Line:       int(startPoint.Row) + 1,
						}
						output.Classes = append(output.Classes, rel)
					}
				}
			}
		}
	}
}

func (e *JavaExtractor) extractInterface(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	annotations := e.extractAnnotations(node, source, lang)

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindInterface,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicJava(node, source, lang),
		Visibility: getVisibilityJava(node, source, lang),
		Decorators: annotations,
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *JavaExtractor) extractEnum(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	annotations := e.extractAnnotations(node, source, lang)

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindEnum,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   isPublicJava(node, source, lang),
		Visibility: getVisibilityJava(node, source, lang),
		Decorators: annotations,
	}
	output.Definitions = append(output.Definitions, def)
}

func (e *JavaExtractor) extractField(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()
	vis := getVisibilityJava(node, source, lang)
	exported := isPublicJava(node, source, lang)
	annotations := e.extractAnnotations(node, source, lang)

	// A field_declaration may have multiple variable_declarator children
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) != "variable_declarator" {
			continue
		}
		name := getChildByFieldNameContent(child, "name", source, lang)
		if name == "" {
			continue
		}
		def := types.Definition{
			Name:       name,
			Kind:       types.SymbolKindField,
			Line:       int(startPoint.Row) + 1,
			Exported:   exported,
			Visibility: vis,
			Decorators: annotations,
		}

		// Detect parent class by walking up the AST
		parent := node.Parent()
		for parent != nil {
			if parent.Type(lang) == "class_declaration" {
				className := getChildByFieldNameContent(parent, "name", source, lang)
				if className != "" {
					def.Parent = className
					def.ParentKind = types.SymbolKindClass
				}
				break
			}
			parent = parent.Parent()
		}

		output.Definitions = append(output.Definitions, def)
	}
}

func (e *JavaExtractor) extractImport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "scoped_identifier" || child.Type(lang) == "identifier" {
			imp := types.Import{
				Source: child.Text(source),
				Line:   int(startPoint.Row) + 1,
			}
			output.Imports = append(output.Imports, imp)
			break
		}
	}
}

func (e *JavaExtractor) extractCall(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	call := types.Call{
		Name:   name,
		Line:   int(startPoint.Row) + 1,
		Column: int(startPoint.Column) + 1,
	}

	obj := node.ChildByFieldName("object", lang)
	if obj != nil {
		call.IsMethod = true
		call.Receiver = obj.Text(source)
	}

	output.Calls = append(output.Calls, call)
}

// isPublicJava checks if a node has a "public" modifier.
// Bug fix: check modifier text content, not node type.
func isPublicJava(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) bool {
	return javaModifierContains(node, source, lang, "public")
}

// getVisibilityJava returns the visibility of a Java node.
// Bug fix: check modifier text content, not node type string.
func getVisibilityJava(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) types.Visibility {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) != "modifiers" {
			continue
		}
		// The modifiers node text is the modifier keyword itself (e.g. "public")
		// but it may also contain multiple keywords; check each child token.
		modText := child.Text(source)
		if strings.Contains(modText, "public") {
			return types.VisibilityPublic
		}
		if strings.Contains(modText, "protected") {
			return types.VisibilityProtected
		}
		if strings.Contains(modText, "private") {
			return types.VisibilityPrivate
		}
	}
	return types.VisibilityInternal
}

// javaModifierContains checks whether any modifier node's text contains the given keyword.
func javaModifierContains(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, keyword string) bool {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == "modifiers" {
			if strings.Contains(child.Text(source), keyword) {
				return true
			}
		}
	}
	return false
}

// extractAnnotations extracts Java annotations from a node's modifiers.
func (e *JavaExtractor) extractAnnotations(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) []types.Decorator {
	var annotations []types.Decorator
	
	// Look for marker_annotation, annotation, or annotation in modifiers
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		
		// Check direct annotation children
		annotation := e.parseAnnotation(child, source, lang)
		if annotation.Name != "" {
			annotations = append(annotations, annotation)
			continue
		}
		
		// Check annotations inside modifiers node
		if child.Type(lang) == "modifiers" {
			for j := 0; j < child.ChildCount(); j++ {
				modChild := child.Child(j)
				if modChild == nil {
					continue
				}
				annotation := e.parseAnnotation(modChild, source, lang)
				if annotation.Name != "" {
					annotations = append(annotations, annotation)
				}
			}
		}
	}
	
	return annotations
}

// parseAnnotation parses a single annotation node.
func (e *JavaExtractor) parseAnnotation(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) types.Decorator {
	nodeType := node.Type(lang)
	
	switch nodeType {
	case "marker_annotation":
		// @Override
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil && child.Type(lang) == "identifier" {
				return types.Decorator{
					Name: child.Text(source),
					Line: int(node.StartPoint().Row) + 1,
				}
			}
		}
	
	case "annotation":
		// @SuppressWarnings("unchecked") or @RequestMapping("/api")
		annotation := types.Decorator{
			Line: int(node.StartPoint().Row) + 1,
		}
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			switch child.Type(lang) {
			case "identifier":
				annotation.Name = child.Text(source)
			case "argument_list":
				annotation.Args = child.Text(source)
			}
		}
		return annotation
	}
	
	return types.Decorator{}
}