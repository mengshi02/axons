package extractors

import (
	"strings"

	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// PythonExtractor extracts symbols from Python source code.
type PythonExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from Python source code.
func (e *PythonExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.PythonLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from Python AST.
func (e *PythonExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	
	nodeType := node.Type(lang)
	
	switch nodeType {
	case "decorated_definition":
		// decorated_definition handles both decorators and the wrapped definition
		// do not recurse into children after this
		e.extractDecoratedDefinition(node, source, lang, output)
		return
	case "function_definition":
		e.extractFunction(node, source, lang, output)
	case "class_definition":
		e.extractClass(node, source, lang, output)
	case "import_statement":
		e.extractImport(node, source, lang, output)
	case "import_from_statement":
		e.extractImportFrom(node, source, lang, output)
	case "call":
		e.extractCall(node, source, lang, output)
	case "assignment":
		e.extractModuleLevelAssignment(node, source, lang, output)
	}

	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}
}

func (e *PythonExtractor) extractFunction(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	// Check if async
	isAsync := false
	for i := 0; i < node.ChildCount(); i++ {
		if node.Child(i) != nil && node.Child(i).Type(lang) == "async" {
			isAsync = true
			break
		}
	}
	
	// Extract type signature (parameters and return type)
	typeSignature := e.extractFunctionTypeSignature(node, source, lang)

	def := types.Definition{
		Name:          name,
		Kind:          types.SymbolKindMethod,
		Line:          int(startPoint.Row) + 1,
		EndLine:       int(endPoint.Row) + 1,
		Exported:      !strings.HasPrefix(name, "_"),
		Visibility:    getVisibilityPython(name),
		IsAsync:       isAsync,
		TypeSignature: typeSignature,
	}

	// Detect parent class by walking up the AST
	parent := node.Parent()
	for parent != nil {
		if parent.Type(lang) == "class_definition" {
			className := getChildByFieldNameContent(parent, "name", source, lang)
			if className != "" {
				def.Parent = className
				def.ParentKind = types.SymbolKindClass
			}
			break
		}
		parent = parent.Parent()
	}

	// If no parent class found, this is a top-level function
	if def.Parent == "" {
		def.Kind = types.SymbolKindFunction
	}

	output.Definitions = append(output.Definitions, def)
}

// extractFunctionTypeSignature extracts the type signature from a function definition.
func (e *PythonExtractor) extractFunctionTypeSignature(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	var params, returnType string
	
	// Find parameters node
	paramsNode := node.ChildByFieldName("parameters", lang)
	if paramsNode != nil {
		params = e.extractParameters(paramsNode, source, lang)
	}
	
	// Find return type
	returnNode := node.ChildByFieldName("return_type", lang)
	if returnNode != nil {
		returnType = strings.TrimSpace(returnNode.Text(source))
	}
	
	// Build signature
	if params != "" && returnType != "" {
		return params + " -> " + strings.TrimPrefix(returnType, "-> ")
	} else if params != "" {
		return params
	}
	
	return ""
}

// extractParameters extracts parameter list with type annotations.
func (e *PythonExtractor) extractParameters(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	var params []string
	
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		
		switch child.Type(lang) {
		case "identifier":
			// Simple parameter without type
			params = append(params, child.Text(source))
		case "typed_parameter", "default_parameter", "typed_default_parameter":
			// Parameter with type annotation
			paramText := e.extractTypedParameter(child, source, lang)
			if paramText != "" {
				params = append(params, paramText)
			}
		case "list_splat_pattern":
			// *args
			params = append(params, child.Text(source))
		case "dictionary_splat_pattern":
			// **kwargs
			params = append(params, child.Text(source))
		}
	}
	
	return "(" + strings.Join(params, ", ") + ")"
}

// extractTypedParameter extracts a typed parameter.
func (e *PythonExtractor) extractTypedParameter(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	return strings.TrimSpace(node.Text(source))
}

func (e *PythonExtractor) extractClass(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
		Exported:   !strings.HasPrefix(name, "_"),
		Visibility: getVisibilityPython(name),
	}
	output.Definitions = append(output.Definitions, def)

	// Extract base classes from argument_list
	// In Python, class Child(Base, Mixin) - first non-ABC base is Extends,
	// others are Implements (mixins / ABCs)
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
		if child.Type(lang) != "argument_list" {
			continue
		}
		for j := 0; j < child.ChildCount(); j++ {
			base := child.Child(j)
			if base == nil {
				continue
			}
			bt := base.Type(lang)
			if bt == "identifier" || bt == "attribute" || bt == "type" {
				baseName := base.Text(source)
				if !firstBaseFound {
					classRelation.Extends = baseName
					firstBaseFound = true
				} else {
					classRelation.Implements = append(classRelation.Implements, baseName)
				}
			}
		}
	}

	if classRelation.Extends != "" || len(classRelation.Implements) > 0 {
		output.Classes = append(output.Classes, classRelation)
	}
}

func (e *PythonExtractor) extractImport(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "dotted_name", "identifier":
			imp := types.Import{
				Source: child.Text(source),
				Line:   int(startPoint.Row) + 1,
			}
			output.Imports = append(output.Imports, imp)
		case "aliased_import":
			name := getChildByFieldNameContent(child, "name", source, lang)
			alias := getChildByFieldNameContent(child, "alias", source, lang)
			imp := types.Import{
				Source:  name,
				Alias:   alias,
				Line:    int(startPoint.Row) + 1,
				IsNamed: true,
			}
			output.Imports = append(output.Imports, imp)
		}
	}
}

func (e *PythonExtractor) extractImportFrom(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	// First child dotted_name/identifier after "from" is the module name
	moduleName := ""
	importStartIdx := 0
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)
		if ct == "from" {
			continue
		}
		if (ct == "dotted_name" || ct == "identifier") && moduleName == "" {
			moduleName = child.Text(source)
			importStartIdx = i + 1
			break
		}
	}

	if moduleName == "" {
		return
	}

	// Bug fix: iterate all remaining children to handle both:
	//   from x import a, b as c      -> dotted_name / aliased_import directly as children
	//   from x import (a, b)         -> import_list wrapping them
	//   from x import *              -> wildcard_import
	seenImport := false
	for i := importStartIdx; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)

		if ct == "import" {
			seenImport = true
			continue
		}
		if !seenImport {
			continue
		}

		switch ct {
		case "dotted_name", "identifier":
			imp := types.Import{
				Source:  moduleName,
				Alias:   child.Text(source),
				Line:    int(startPoint.Row) + 1,
				IsNamed: true,
			}
			output.Imports = append(output.Imports, imp)
		case "aliased_import":
			iname := getChildByFieldNameContent(child, "name", source, lang)
			alias := getChildByFieldNameContent(child, "alias", source, lang)
			imp := types.Import{
				Source:  moduleName + "." + iname,
				Alias:   alias,
				Line:    int(startPoint.Row) + 1,
				IsNamed: true,
			}
			output.Imports = append(output.Imports, imp)
		case "import_list":
			for j := 0; j < child.ChildCount(); j++ {
				spec := child.Child(j)
				if spec == nil {
					continue
				}
				switch spec.Type(lang) {
				case "dotted_name", "identifier":
					imp := types.Import{
						Source:  moduleName,
						Alias:   spec.Text(source),
						Line:    int(startPoint.Row) + 1,
						IsNamed: true,
					}
					output.Imports = append(output.Imports, imp)
				case "aliased_import":
					iname := getChildByFieldNameContent(spec, "name", source, lang)
					alias := getChildByFieldNameContent(spec, "alias", source, lang)
					imp := types.Import{
						Source:  moduleName + "." + iname,
						Alias:   alias,
						Line:    int(startPoint.Row) + 1,
						IsNamed: true,
					}
					output.Imports = append(output.Imports, imp)
				}
			}
		case "wildcard_import":
			imp := types.Import{
				Source: moduleName,
				Alias:  "*",
				Line:   int(startPoint.Row) + 1,
			}
			output.Imports = append(output.Imports, imp)
		}
	}
}

func (e *PythonExtractor) extractCall(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
	case "attribute":
		attr := funcNode.ChildByFieldName("attribute", lang)
		obj := funcNode.ChildByFieldName("object", lang)
		if attr != nil {
			call.Name = attr.Text(source)
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

// extractModuleLevelAssignment extracts module-level constant assignments.
// Bug fix: original extractAssignment was called on all "assignment" nodes including
// local variables inside functions, causing noise. Now only ALL_CAPS names are emitted.
func (e *PythonExtractor) extractModuleLevelAssignment(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	// node is an "assignment" node directly
	left := node.ChildByFieldName("left", lang)
	if left == nil || left.Type(lang) != "identifier" {
		return
	}
	name := left.Text(source)
	// Only emit ALL_CAPS constants to reduce noise from local variables
	if !isAllCaps(name) {
		return
	}
	startPoint := node.StartPoint()
	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindConstant,
		Line:       int(startPoint.Row) + 1,
		Exported:   !strings.HasPrefix(name, "_"),
		Visibility: getVisibilityPython(name),
	}
	output.Definitions = append(output.Definitions, def)
}

// isAllCaps returns true if s is an ALL_CAPS identifier (Python constant convention).
func isAllCaps(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c == '_' {
			continue
		}
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func getVisibilityPython(name string) types.Visibility {
	if strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") {
		return types.VisibilityPublic
	}
	if strings.HasPrefix(name, "__") {
		return types.VisibilityPrivate
	}
	if strings.HasPrefix(name, "_") {
		return types.VisibilityProtected
	}
	return types.VisibilityPublic
}

// extractDecoratedDefinition handles decorated functions and classes.
func (e *PythonExtractor) extractDecoratedDefinition(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	// Extract decorators first
	decorators := e.extractDecorators(node, source, lang)
	
	// Find the actual definition (function or class)
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		childType := child.Type(lang)
		
		// Skip decorator nodes
		if childType == "decorator" {
			continue
		}
		
		// Process the actual definition
		switch childType {
		case "function_definition":
			e.extractFunctionWithDecorators(child, source, lang, output, decorators)
		case "class_definition":
			e.extractClassWithDecorators(child, source, lang, output, decorators)
		}
	}
}

// extractDecorators extracts all decorators from a decorated definition.
func (e *PythonExtractor) extractDecorators(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) []types.Decorator {
	var decorators []types.Decorator
	
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil || child.Type(lang) != "decorator" {
			continue
		}
		
		decorator := e.parseDecorator(child, source, lang)
		if decorator.Name != "" {
			decorators = append(decorators, decorator)
		}
	}
	
	return decorators
}

// parseDecorator parses a single decorator node.
func (e *PythonExtractor) parseDecorator(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) types.Decorator {
	startPoint := node.StartPoint()
	
	// Find the decorator name (skip the @ symbol)
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		
		childType := child.Type(lang)
		
		switch childType {
		case "identifier":
			// Simple decorator: @dataclass
			return types.Decorator{
				Name: child.Text(source),
				Line: int(startPoint.Row) + 1,
			}
		
		case "attribute":
			// Attribute decorator: @auth.required
			return types.Decorator{
				Name: child.Text(source),
				Line: int(startPoint.Row) + 1,
			}
		
		case "call":
			// Call decorator: @app.route("/api")
			decorator := types.Decorator{
				Line: int(startPoint.Row) + 1,
			}
			
			// Extract the function name being called
			for j := 0; j < child.ChildCount(); j++ {
				callChild := child.Child(j)
				if callChild == nil {
					continue
				}
				
				if callChild.Type(lang) == "attribute" || callChild.Type(lang) == "identifier" {
					decorator.Name = callChild.Text(source)
				} else if callChild.Type(lang) == "argument_list" {
					// Extract arguments
					decorator.Args = callChild.Text(source)
				}
			}
			
			return decorator
		}
	}
	
	return types.Decorator{}
}

// extractFunctionWithDecorators extracts a function definition with decorators.
func (e *PythonExtractor) extractFunctionWithDecorators(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput, decorators []types.Decorator) {
	name := getChildByFieldNameContent(node, "name", source, lang)
	if name == "" {
		return
	}

	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	// Check if async
	isAsync := false
	for i := 0; i < node.ChildCount(); i++ {
		if node.Child(i) != nil && node.Child(i).Type(lang) == "async" {
			isAsync = true
			break
		}
	}
	
	// Extract type signature
	typeSignature := e.extractFunctionTypeSignature(node, source, lang)

	def := types.Definition{
		Name:          name,
		Kind:          types.SymbolKindMethod,
		Line:          int(startPoint.Row) + 1,
		EndLine:       int(endPoint.Row) + 1,
		Exported:      !strings.HasPrefix(name, "_"),
		Visibility:    getVisibilityPython(name),
		Decorators:    decorators,
		IsAsync:       isAsync,
		TypeSignature: typeSignature,
	}

	// Detect parent class by walking up the AST
	parent := node.Parent()
	for parent != nil {
		if parent.Type(lang) == "class_definition" {
			className := getChildByFieldNameContent(parent, "name", source, lang)
			if className != "" {
				def.Parent = className
				def.ParentKind = types.SymbolKindClass
			}
			break
		}
		// Also check decorated_definition wrapper
		if parent.Type(lang) == "decorated_definition" {
			parent = parent.Parent()
			continue
		}
		parent = parent.Parent()
	}

	// If no parent class found, this is a top-level function
	if def.Parent == "" {
		def.Kind = types.SymbolKindFunction
	}

	output.Definitions = append(output.Definitions, def)
}

// extractClassWithDecorators extracts a class definition with decorators.
func (e *PythonExtractor) extractClassWithDecorators(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput, decorators []types.Decorator) {
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
		Exported:   !strings.HasPrefix(name, "_"),
		Visibility: getVisibilityPython(name),
		Decorators: decorators,
	}
	output.Definitions = append(output.Definitions, def)

	// Extract base classes
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
		if child.Type(lang) != "argument_list" {
			continue
		}
		for j := 0; j < child.ChildCount(); j++ {
			base := child.Child(j)
			if base == nil {
				continue
			}
			bt := base.Type(lang)
			if bt == "identifier" || bt == "attribute" || bt == "type" {
				baseName := base.Text(source)
				if !firstBaseFound {
					classRelation.Extends = baseName
					firstBaseFound = true
				} else {
					classRelation.Implements = append(classRelation.Implements, baseName)
				}
			}
		}
	}
	if classRelation.Extends != "" || len(classRelation.Implements) > 0 {
		output.Classes = append(output.Classes, classRelation)
	}
}