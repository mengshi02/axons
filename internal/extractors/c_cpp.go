package extractors

import (
	"strings"

	"github.com/mengshi02/axons/pkg/types"
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// CExtractor extracts symbols from C source code.
type CExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from C source code.
func (e *CExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.CLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from C AST.
func (e *CExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	switch node.Type(lang) {
	case "function_definition":
		extractCFunction(node, source, lang, output)
	case "type_definition":
		extractCTypedef(node, source, lang, output)
	case "struct_specifier", "union_specifier":
		extractCStruct(node, source, lang, output)
	case "enum_specifier":
		extractCEnum(node, source, lang, output)
	case "preproc_include":
		extractCInclude(node, source, lang, output)
	case "preproc_def":
		extractCPreprocDefine(node, source, lang, output)
	case "preproc_function_def":
		extractCPreprocFunctionDef(node, source, lang, output)
	case "declaration":
		extractCDeclaration(node, source, lang, output)
	case "call_expression":
		extractCCall(node, source, lang, output)
	}

	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}
}

// CppExtractor extracts symbols from C++ source code.
type CppExtractor struct {
	BaseExtractor
}

// Extract extracts definitions, calls, imports, etc. from C++ source code.
func (e *CppExtractor) Extract(source []byte, filePath string) (*types.ExtractorOutput, error) {
	return extractWithLanguage(source, grammars.CppLanguage(), e.extractNodes)
}

// extractNodes extracts nodes from C++ AST.
func (e *CppExtractor) extractNodes(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	if node == nil {
		return
	}
	switch node.Type(lang) {
	case "function_definition":
		extractCFunction(node, source, lang, output)
	case "type_definition":
		extractCTypedef(node, source, lang, output)
	case "struct_specifier", "union_specifier":
		extractCStruct(node, source, lang, output)
	case "enum_specifier":
		extractCEnum(node, source, lang, output)
	case "class_specifier":
		extractCppClass(node, source, lang, output)
	case "preproc_include":
		extractCInclude(node, source, lang, output)
	case "preproc_def":
		extractCPreprocDefine(node, source, lang, output)
	case "preproc_function_def":
		extractCPreprocFunctionDef(node, source, lang, output)
	case "declaration":
		extractCDeclaration(node, source, lang, output)
	case "call_expression":
		extractCCall(node, source, lang, output)
	case "namespace_definition":
		extractCppNamespace(node, source, lang, output)
	case "template_declaration":
		extractCppTemplate(node, source, lang, output)
	}

	for i := 0; i < node.ChildCount(); i++ {
		e.extractNodes(node.Child(i), source, lang, output)
	}
}

// extractCFunction extracts a function definition from C/C++ AST.
func extractCFunction(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	name := extractFunctionName(node, source, lang)
	if name == "" {
		return
	}

	// Detect if it's a method (has a qualified name with "::")
	isMethod := strings.Contains(name, "::")
	parent := ""
	kind := types.SymbolKindFunction
	if isMethod {
		parts := strings.SplitN(name, "::", 2)
		parent = parts[0]
		name = parts[1]
		kind = types.SymbolKindMethod
	}

	def := types.Definition{
		Name:       name,
		Kind:       kind,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
		Parent:     parent,
	}
	if isMethod {
		def.ParentKind = types.SymbolKindClass
	}
	output.Definitions = append(output.Definitions, def)
}

// extractFunctionName extracts the function name from a function_definition node.
// It handles declarators like: int foo(...), int* foo(...), void (*fp)(...).
func extractFunctionName(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	declarator := node.ChildByFieldName("declarator", lang)
	if declarator == nil {
		return ""
	}
	return extractDeclaratorName(declarator, source, lang)
}

// extractDeclaratorName recursively unwraps declarator nodes to find the function name.
func extractDeclaratorName(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	if node == nil {
		return ""
	}
	t := node.Type(lang)
	switch t {
	case "function_declarator":
		inner := node.ChildByFieldName("declarator", lang)
		return extractDeclaratorName(inner, source, lang)
	case "pointer_declarator":
		inner := node.ChildByFieldName("declarator", lang)
		return extractDeclaratorName(inner, source, lang)
	case "identifier":
		return node.Text(source)
	case "field_identifier":
		return node.Text(source)
	case "qualified_identifier":
		// C++ qualified: ClassName::methodName
		return node.Text(source)
	case "destructor_name":
		return node.Text(source)
	case "operator_name":
		return node.Text(source)
	}
	return ""
}

// extractCTypedef extracts typedef declarations (type_definition nodes).
func extractCTypedef(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	// Find the last type_identifier as the typedef name.
	// In tree-sitter C grammar, type_definition children are:
	//   "typedef" keyword, type node, type_identifier (alias name), ";"
	var typedefName string
	for i := node.ChildCount() - 1; i >= 0; i-- {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)
		if ct == "type_identifier" || ct == "identifier" {
			typedefName = child.Text(source)
			break
		}
	}

	if typedefName == "" {
		return
	}

	def := types.Definition{
		Name:       typedefName,
		Kind:       types.SymbolKindType,
		Line:       int(startPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
	}
	output.Definitions = append(output.Definitions, def)
}

// extractCStruct extracts struct/union declarations.
func extractCStruct(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil {
		return
	}

	name := nameNode.Text(source)
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindStruct,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
	}
	output.Definitions = append(output.Definitions, def)
}

// extractCEnum extracts enum declarations.
func extractCEnum(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil {
		return
	}

	name := nameNode.Text(source)
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindEnum,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
	}
	output.Definitions = append(output.Definitions, def)
}

// extractCppClass extracts C++ class declarations.
func extractCppClass(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil {
		return
	}

	name := nameNode.Text(source)
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindClass,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
	}
	output.Definitions = append(output.Definitions, def)

	// Extract base classes from base_class_clause child node
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) != "base_class_clause" {
			continue
		}
		for j := 0; j < child.ChildCount(); j++ {
			baseChild := child.Child(j)
			if baseChild == nil {
				continue
			}
			bt := baseChild.Type(lang)
			if bt == "type_identifier" || bt == "qualified_identifier" {
				rel := types.ClassRelation{
					ClassName: name,
					Extends:   baseChild.Text(source),
					Line:      int(startPoint.Row) + 1,
				}
				output.Classes = append(output.Classes, rel)
			}
		}
	}
}

// extractCppNamespace extracts C++ namespace definitions.
func extractCppNamespace(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil {
		return
	}

	name := nameNode.Text(source)
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindModule,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
	}
	output.Definitions = append(output.Definitions, def)
}

// extractCppTemplate extracts C++ template declarations (template functions/classes).
func extractCppTemplate(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Type(lang) {
		case "function_definition":
			extractCFunction(child, source, lang, output)
		case "class_specifier":
			extractCppClass(child, source, lang, output)
		}
	}
}

// extractCInclude extracts #include directives.
func extractCInclude(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)
		if ct == "string_literal" || ct == "system_lib_string" {
			path := child.Text(source)
			// Strip surrounding quotes/angle-brackets
			if len(path) >= 2 {
				path = path[1 : len(path)-1]
			}
			imp := types.Import{
				Source: path,
				Line:   int(startPoint.Row) + 1,
			}
			output.Imports = append(output.Imports, imp)
		}
	}
}

// extractCPreprocDefine extracts #define macro constants.
// tree-sitter C grammar: preproc_define -> "define" identifier value
func extractCPreprocDefine(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	// Find the name identifier
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)
		// In tree-sitter C grammar, the name is an "identifier" child
		if ct == "identifier" {
			name = child.Text(source)
			break
		}
	}

	if name == "" {
		return
	}

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindConstant,
		Line:       int(startPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
	}
	output.Definitions = append(output.Definitions, def)
}

// extractCPreprocFunctionDef extracts #define macro functions.
// tree-sitter C grammar: preproc_function_def -> "define" identifier preproc_params value
func extractCPreprocFunctionDef(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()

	// Find the name identifier
	var name string
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)
		// In tree-sitter C grammar, the name is an "identifier" child before preproc_params
		if ct == "identifier" {
			name = child.Text(source)
			break
		}
	}

	if name == "" {
		return
	}

	def := types.Definition{
		Name:       name,
		Kind:       types.SymbolKindFunction,
		Line:       int(startPoint.Row) + 1,
		EndLine:    int(endPoint.Row) + 1,
		Exported:   true,
		Visibility: types.VisibilityPublic,
	}
	output.Definitions = append(output.Definitions, def)
}

// extractCDeclaration extracts global variable declarations.
// tree-sitter C grammar: declaration -> type_specifier declarator_list
// We extract file-scope declarations, including those inside #ifdef/#if blocks.
func extractCDeclaration(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	// Only process file-scope declarations. These can be direct children of
	// translation_unit, or inside preprocessor conditional blocks (#ifdef, #if, etc.)
	// which are at file scope. We do NOT want local variables inside function bodies.
	if !isFileScopeDeclaration(node, lang) {
		return
	}

	// Skip function declarations (forward declarations) — we only want variables.
	// A function declaration has a function_declarator as a direct child.
	if isFunctionDeclaration(node, lang) {
		extractCFunctionDeclaration(node, source, lang, output)
		return
	}

	startPoint := node.StartPoint()

	// Walk through the declaration's children to find variable names.
	// Common patterns:
	//   int x = 0;               -> declaration { primitive_type, init_declarator { identifier, =, value }, ; }
	//   struct Foo s;             -> declaration { struct_specifier, identifier, ; }
	//   char* buf[256];           -> declaration { primitive_type, pointer_declarator, ; }
	//   int a, b;                 -> declaration { primitive_type, init_declarator, ,, init_declarator, ; }
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)

		switch ct {
		case "init_declarator":
			// init_declarator -> declarator = value
			varName := extractDeclaratorNameFromDeclaration(child, source, lang)
			if varName != "" {
				def := types.Definition{
					Name:       varName,
					Kind:       types.SymbolKindVariable,
					Line:       int(startPoint.Row) + 1,
					Exported:   true,
					Visibility: types.VisibilityPublic,
				}
				output.Definitions = append(output.Definitions, def)
			}
		case "identifier":
			// Direct identifier child: e.g., "struct redisServer server;" has
			// declaration { struct_specifier, identifier(server), ; }
			varName := child.Text(source)
			if varName != "" && !definitionNameExists(output, varName, types.SymbolKindVariable) {
				def := types.Definition{
					Name:       varName,
					Kind:       types.SymbolKindVariable,
					Line:       int(startPoint.Row) + 1,
					Exported:   true,
					Visibility: types.VisibilityPublic,
				}
				output.Definitions = append(output.Definitions, def)
			}
		case "pointer_declarator", "array_declarator":
			// Declarations like "char *buf;" or "int arr[10];"
			varName := extractDeclaratorNameFromDeclaration(child, source, lang)
			if varName != "" && !definitionNameExists(output, varName, types.SymbolKindVariable) {
				def := types.Definition{
					Name:       varName,
					Kind:       types.SymbolKindVariable,
					Line:       int(startPoint.Row) + 1,
					Exported:   true,
					Visibility: types.VisibilityPublic,
				}
				output.Definitions = append(output.Definitions, def)
			}
		}
	}
}

// isFileScopeDeclaration checks if a declaration node is at file scope.
// File-scope declarations can be direct children of translation_unit,
// or inside preprocessor conditional blocks (#ifdef, #if, etc.) that are
// themselves at file scope.
func isFileScopeDeclaration(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}
	parentType := parent.Type(lang)

	// Direct child of translation_unit — definitely file scope
	if parentType == "translation_unit" {
		return true
	}

	// Inside a preprocessor conditional block — also file scope if the
	// preproc block itself is at file scope.
	preprocBlockTypes := map[string]bool{
		"preproc_if":     true,
		"preproc_ifdef":  true,
		"preproc_elif":   true,
		"preproc_else":   true,
		"preproc_elifdef": true,
	}
	if preprocBlockTypes[parentType] {
		// Check if the preproc block is at file scope
		return isFileScopePreprocBlock(parent, lang)
	}

	return false
}

// isFileScopePreprocBlock checks if a preprocessor block is at file scope.
func isFileScopePreprocBlock(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}
	parentType := parent.Type(lang)
	if parentType == "translation_unit" {
		return true
	}
	// Nested preproc blocks
	preprocBlockTypes := map[string]bool{
		"preproc_if":     true,
		"preproc_ifdef":  true,
		"preproc_elif":   true,
		"preproc_else":   true,
		"preproc_elifdef": true,
	}
	if preprocBlockTypes[parentType] {
		return isFileScopePreprocBlock(parent, lang)
	}
	return false
}

// isFunctionDeclaration checks if a declaration is a function forward declaration
// (not a variable declaration). We skip these to avoid duplicates with function_definition.
func isFunctionDeclaration(node *gotreesitter.Node, lang *gotreesitter.Language) bool {
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)
		// Direct function_declarator child means this is a function declaration
		if ct == "function_declarator" {
			return true
		}
		// init_declarator containing a function_declarator
		if ct == "init_declarator" {
			for j := 0; j < child.ChildCount(); j++ {
				grandchild := child.Child(j)
				if grandchild != nil && grandchild.Type(lang) == "function_declarator" {
					return true
				}
			}
		}
	}
	return false
}

// extractCFunctionDeclaration extracts function forward declarations.
// These are common in C header files: e.g., "void ngx_init_cycle(ngx_cycle_t *cycle);"
// They are represented as: declaration -> type_specifier function_declarator
func extractCFunctionDeclaration(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
	startPoint := node.StartPoint()

	// Find the function_declarator and extract the function name
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		ct := child.Type(lang)

		var funcDeclarator *gotreesitter.Node
		if ct == "function_declarator" {
			funcDeclarator = child
		} else if ct == "pointer_declarator" {
			// Function pointer declaration like "void (*handler)(int);"
			// Check if inner is function_declarator
			for j := 0; j < child.ChildCount(); j++ {
				inner := child.Child(j)
				if inner != nil && inner.Type(lang) == "function_declarator" {
					funcDeclarator = inner
					break
				}
			}
		}

		if funcDeclarator != nil {
			name := extractDeclaratorName(funcDeclarator, source, lang)
			if name == "" {
				return
			}

			// Detect if it's a method (has a qualified name with "::")
			isMethod := strings.Contains(name, "::")
			parent := ""
			kind := types.SymbolKindFunction
			if isMethod {
				parts := strings.SplitN(name, "::", 2)
				parent = parts[0]
				name = parts[1]
				kind = types.SymbolKindMethod
			}

			def := types.Definition{
				Name:       name,
				Kind:       kind,
				Line:       int(startPoint.Row) + 1,
				EndLine:    int(startPoint.Row) + 1, // Forward declarations are single-line
				Exported:   true,
				Visibility: types.VisibilityPublic,
				Parent:     parent,
			}
			if isMethod {
				def.ParentKind = types.SymbolKindClass
			}
			output.Definitions = append(output.Definitions, def)
			return
		}
	}
}

// extractDeclaratorNameFromDeclaration recursively unwraps declarator nodes to find the variable name.
func extractDeclaratorNameFromDeclaration(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language) string {
	if node == nil {
		return ""
	}
	t := node.Type(lang)
	switch t {
	case "init_declarator":
		// Unwrap to the declarator child
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			ct := child.Type(lang)
			if ct == "pointer_declarator" || ct == "array_declarator" || ct == "function_declarator" || ct == "identifier" {
				return extractDeclaratorNameFromDeclaration(child, source, lang)
			}
		}
		return ""
	case "pointer_declarator":
		inner := node.ChildByFieldName("declarator", lang)
		if inner != nil {
			return extractDeclaratorNameFromDeclaration(inner, source, lang)
		}
		// Fallback: iterate children
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil && child.Type(lang) == "identifier" {
				return child.Text(source)
			}
		}
		return ""
	case "array_declarator":
		inner := node.ChildByFieldName("declarator", lang)
		if inner != nil {
			return extractDeclaratorNameFromDeclaration(inner, source, lang)
		}
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil && child.Type(lang) == "identifier" {
				return child.Text(source)
			}
		}
		return ""
	case "function_declarator":
		// This is a function declaration, not a variable - skip
		return ""
	case "identifier":
		return node.Text(source)
	case "field_identifier":
		return node.Text(source)
	}
	return ""
}

// definitionNameExists checks if a definition with the given name and kind already exists.
func definitionNameExists(output *types.ExtractorOutput, name string, kind types.SymbolKind) bool {
	for _, d := range output.Definitions {
		if d.Name == name && d.Kind == kind {
			return true
		}
	}
	return false
}

// extractCCall extracts function call expressions.
func extractCCall(node *gotreesitter.Node, source []byte, lang *gotreesitter.Language, output *types.ExtractorOutput) {
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
	case "field_expression":
		field := funcNode.ChildByFieldName("field", lang)
		obj := funcNode.ChildByFieldName("argument", lang)
		if field != nil {
			call.Name = field.Text(source)
			call.IsMethod = true
			if obj != nil {
				call.Receiver = obj.Text(source)
			}
		}
	case "qualified_identifier":
		call.Name = funcNode.Text(source)
	default:
		return
	}

	if call.Name != "" {
		output.Calls = append(output.Calls, call)
	}
}
