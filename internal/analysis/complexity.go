// Package analysis provides code analysis capabilities.
package analysis

import (
	"strings"
	"unicode"
)

// ComplexityMetrics holds complexity measurements for a function.
type ComplexityMetrics struct {
	NodeID          int64   `json:"node_id"`
	Cyclomatic      int     `json:"cyclomatic"`       // 圈复杂度
	Cognitive       int     `json:"cognitive"`        // 认知复杂度
	Nesting         int     `json:"nesting"`          // 最大嵌套深度
	LinesOfCode     int     `json:"lines_of_code"`    // 代码行数
	LinesOfComments int     `json:"lines_of_comments"` // 注释行数
	ParameterCount  int     `json:"parameter_count"`  // 参数数量
	// Halstead metrics
	TotalOperators   int     `json:"total_operators"`
	TotalOperands    int     `json:"total_operands"`
	UniqueOperators  int     `json:"unique_operators"`
	UniqueOperands   int     `json:"unique_operands"`
	HalsteadVolume   float64 `json:"halstead_volume"`
	HalsteadDifficulty float64 `json:"halstead_difficulty"`
	HalsteadEffort   float64 `json:"halstead_effort"`
	HalsteadTime     float64 `json:"halstead_time"`
	HalsteadBugs     float64 `json:"halstead_bugs"`
}

// ComplexityAnalyzer analyzes code complexity.
type ComplexityAnalyzer struct {
	// Language-specific keywords
	controlFlowKeywords map[string]bool
	booleanOperators    map[string]bool
}

// NewComplexityAnalyzer creates a new ComplexityAnalyzer.
func NewComplexityAnalyzer() *ComplexityAnalyzer {
	return &ComplexityAnalyzer{
		controlFlowKeywords: map[string]bool{
			// Common control flow keywords
			"if": true, "else": true, "elif": true, "for": true,
			"while": true, "do": true, "switch": true, "case": true,
			"catch": true, "try": true, "except": true, "finally": true,
			"when": true, "match": true, "select": true,
			// Go specific
			"goto": true,
			// Rust specific
			"loop": true,
		},
		booleanOperators: map[string]bool{
			"&&": true, "||": true, "and": true, "or": true,
			"!": true, "not": true, "?": true,
		},
	}
}

// Analyze analyzes the complexity of a function from its source code.
func (ca *ComplexityAnalyzer) Analyze(sourceCode string, language string) *ComplexityMetrics {
	metrics := &ComplexityMetrics{
		LinesOfCode: ca.countLinesOfCode(sourceCode),
	}

	// Calculate cyclomatic complexity
	metrics.Cyclomatic = ca.calculateCyclomatic(sourceCode, language)

	// Calculate cognitive complexity
	metrics.Cognitive = ca.calculateCognitive(sourceCode, language)

	// Calculate nesting depth
	metrics.Nesting = ca.calculateNesting(sourceCode, language)

	// Calculate Halstead metrics
	ca.calculateHalstead(sourceCode, language, metrics)

	return metrics
}

// countLinesOfCode counts non-empty, non-comment lines.
func (ca *ComplexityAnalyzer) countLinesOfCode(source string) int {
	lines := strings.Split(source, "\n")
	loc := 0
	inBlockComment := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if trimmed == "" {
			continue
		}

		// Handle block comments
		if strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "/*") {
			inBlockComment = true
		}
		if inBlockComment {
			if strings.Contains(trimmed, "*/") {
				inBlockComment = false
			}
			continue
		}

		// Skip single-line comments
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "--") {
			continue
		}

		loc++
	}

	return loc
}

// calculateCyclomatic calculates cyclomatic complexity.
// McCabe's cyclomatic complexity = 1 + number of decisions
func (ca *ComplexityAnalyzer) calculateCyclomatic(source string, language string) int {
	complexity := 1 // Base complexity

	// Count decision points
	decisionKeywords := []string{
		"if ", "if(", "else if", "elif ", "elif(",
		"for ", "for(", "while ", "while(",
		"switch ", "switch(", "case ", "catch ",
		"catch(", "except ", "except(",
		"&&", "||", "and ", "or ",
		"?", // Ternary operator
	}

	// Language-specific additions
	switch language {
	case "go":
		decisionKeywords = append(decisionKeywords, "select ", "select{")
	case "rust":
		decisionKeywords = append(decisionKeywords, "match ", "match{", "loop ")
	case "python":
		decisionKeywords = append(decisionKeywords, "for ", "in ")
	}

	for _, keyword := range decisionKeywords {
		complexity += strings.Count(source, keyword)
	}

	return complexity
}

// calculateCognitive calculates cognitive complexity.
// Cognitive complexity penalizes nesting and breaks in control flow.
func (ca *ComplexityAnalyzer) calculateCognitive(source string, language string) int {
	complexity := 0
	nestingLevel := 0

	lines := strings.Split(source, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for control flow keywords
		if ca.isControlFlowStart(trimmed, language) {
			// Add complexity for control flow + nesting penalty
			complexity += 1 + nestingLevel
			nestingLevel++
		}

		// Check for else/elif (no additional nesting, but complexity)
		if ca.isElseBranch(trimmed, language) {
			complexity += 1
		}

		// Check for closing braces to decrease nesting
		if ca.isBlockEnd(trimmed, language) {
			if nestingLevel > 0 {
				nestingLevel--
			}
		}

		// Count boolean operators (each adds complexity)
		for _, op := range []string{"&&", "||", "and ", "or "} {
			complexity += strings.Count(trimmed, op)
		}
	}

	return complexity
}

// isControlFlowStart checks if a line starts a control flow structure.
func (ca *ComplexityAnalyzer) isControlFlowStart(line string, language string) bool {
	controlStarts := []string{
		"if ", "if(", "for ", "for(", "while ", "while(",
		"switch ", "switch(", "try ", "try{", "catch ", "catch(",
		"except ", "except(", "loop ", "loop{", "select ", "select{",
	}

	for _, start := range controlStarts {
		if strings.HasPrefix(line, start) {
			return true
		}
	}

	return false
}

// isElseBranch checks if a line is an else or elif branch.
func (ca *ComplexityAnalyzer) isElseBranch(line string, language string) bool {
	elseKeywords := []string{
		"else ", "else{", "else if", "elif ", "elif(",
		"except ", "except(", "case ", "default ",
	}

	for _, kw := range elseKeywords {
		if strings.HasPrefix(line, kw) {
			return true
		}
	}

	return false
}

// isBlockEnd checks if a line ends a block.
func (ca *ComplexityAnalyzer) isBlockEnd(line string, language string) bool {
	// Most languages use }
	if line == "}" || strings.HasSuffix(line, "}") {
		return true
	}

	// Python uses dedent (harder to detect, simplified)
	if language == "python" {
		// Python blocks end when indentation decreases
		// This is a simplified check
	}

	return false
}

// calculateNesting calculates maximum nesting depth.
func (ca *ComplexityAnalyzer) calculateNesting(source string, language string) int {
	maxNesting := 0
	currentNesting := 0

	lines := strings.Split(source, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Count opening braces
		for _, ch := range trimmed {
			if ch == '{' {
				currentNesting++
				if currentNesting > maxNesting {
					maxNesting = currentNesting
				}
			}
			if ch == '}' {
				if currentNesting > 0 {
					currentNesting--
				}
			}
		}

		// Python-style nesting (based on indentation)
		if language == "python" {
			indent := 0
			for _, ch := range line {
				if ch == ' ' || ch == '\t' {
					indent++
				} else {
					break
				}
			}
			// Python typically uses 4 spaces per indent level
			pythonNesting := indent / 4
			if pythonNesting > maxNesting {
				maxNesting = pythonNesting
			}
		}
	}

	return maxNesting
}

// calculateHalstead calculates Halstead complexity metrics.
func (ca *ComplexityAnalyzer) calculateHalstead(source string, language string, metrics *ComplexityMetrics) {
	operators := make(map[string]int)
	operands := make(map[string]int)

	// Define operators by language
	operatorSet := ca.getOperators(language)

	// Tokenize and classify
	tokens := ca.tokenize(source)

	for _, token := range tokens {
		if operatorSet[token] {
			operators[token]++
			metrics.TotalOperators++
		} else if isIdentifier(token) || isLiteral(token) {
			operands[token]++
			metrics.TotalOperands++
		}
	}

	metrics.UniqueOperators = len(operators)
	metrics.UniqueOperands = len(operands)

	// Calculate Halstead metrics
	// n1 = unique operators, n2 = unique operands
	// N1 = total operators, N2 = total operands
	n1 := float64(metrics.UniqueOperators)
	n2 := float64(metrics.UniqueOperands)
	N1 := float64(metrics.TotalOperators)
	N2 := float64(metrics.TotalOperands)

	// Program vocabulary: n = n1 + n2
	n := n1 + n2
	// Program length: N = N1 + N2
	N := N1 + N2

	// Volume: V = N * log2(n)
	if n > 0 {
		metrics.HalsteadVolume = N * log2(n)
	}

	// Difficulty: D = (n1/2) * (N2/n2)
	if n2 > 0 {
		metrics.HalsteadDifficulty = (n1 / 2) * (N2 / n2)
	}

	// Effort: E = D * V
	metrics.HalsteadEffort = metrics.HalsteadDifficulty * metrics.HalsteadVolume

	// Time to implement (seconds): T = E / 18
	metrics.HalsteadTime = metrics.HalsteadEffort / 18

	// Estimated bugs: B = V / 3000
	metrics.HalsteadBugs = metrics.HalsteadVolume / 3000
}

// getOperators returns language-specific operators.
func (ca *ComplexityAnalyzer) getOperators(language string) map[string]bool {
	common := map[string]bool{
		// Arithmetic
		"+": true, "-": true, "*": true, "/": true, "%": true,
		"++": true, "--": true,
		// Comparison
		"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
		// Logical
		"&&": true, "||": true, "!": true,
		// Bitwise
		"&": true, "|": true, "^": true, "~": true, "<<": true, ">>": true,
		// Assignment
		"=": true, "+=": true, "-=": true, "*=": true, "/=": true,
		// Other
		"->": true, "=>": true, "::": true, ".": true, ":": true,
		"(": true, ")": true, "[": true, "]": true, "{": true, "}": true,
		",": true, ";": true,
	}

	// Add language-specific keywords
	keywords := map[string]bool{
		"if": true, "else": true, "for": true, "while": true,
		"do": true, "switch": true, "case": true, "default": true,
		"break": true, "continue": true, "return": true, "throw": true,
		"try": true, "catch": true, "finally": true,
		"new": true, "delete": true, "typeof": true, "instanceof": true,
		"in": true, "of": true,
		// Go specific
		"go": true, "defer": true, "chan": true, "select": true,
		"range": true, "make": true, "append": true, "len": true,
		// Rust specific
		"let": true, "mut": true, "match": true, "loop": true,
		"unsafe": true, "async": true, "await": true,
	}

	for k, v := range keywords {
		common[k] = v
	}

	return common
}

// tokenize splits source code into tokens.
func (ca *ComplexityAnalyzer) tokenize(source string) []string {
	var tokens []string
	var currentToken strings.Builder

	inString := false
	stringChar := rune(0)
	inComment := false

	for i, ch := range source {
		// Handle string literals
		if inString {
			currentToken.WriteRune(ch)
			if ch == stringChar && i > 0 && source[i-1] != '\\' {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
				inString = false
			}
			continue
		}

		// Handle comments
		if inComment {
			if ch == '\n' {
				inComment = false
			}
			continue
		}

		// Check for string start
		if ch == '"' || ch == '\'' || ch == '`' {
			if currentToken.Len() > 0 {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
			}
			currentToken.WriteRune(ch)
			inString = true
			stringChar = ch
			continue
		}

		// Check for comment start
		if ch == '/' && i+1 < len(source) {
			next := rune(source[i+1])
			if next == '/' || next == '*' {
				inComment = true
				continue
			}
		}

		// Check for operators
		if isOperatorChar(ch) {
			if currentToken.Len() > 0 {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
			}
			// Check for multi-char operators
			if i+1 < len(source) && isOperatorChar(rune(source[i+1])) {
				twoChar := string([]rune{ch, rune(source[i+1])})
				if isTwoCharOperator(twoChar) {
					tokens = append(tokens, twoChar)
					continue
				}
			}
			tokens = append(tokens, string(ch))
			continue
		}

		// Check for whitespace
		if unicode.IsSpace(ch) {
			if currentToken.Len() > 0 {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
			}
			continue
		}

		// Check for punctuation
		if isPunctuation(ch) {
			if currentToken.Len() > 0 {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
			}
			tokens = append(tokens, string(ch))
			continue
		}

		// Add to current token
		currentToken.WriteRune(ch)
	}

	// Don't forget the last token
	if currentToken.Len() > 0 {
		tokens = append(tokens, currentToken.String())
	}

	return tokens
}

// isOperatorChar checks if a character could be part of an operator.
func isOperatorChar(ch rune) bool {
	return ch == '+' || ch == '-' || ch == '*' || ch == '/' ||
		ch == '%' || ch == '=' || ch == '!' || ch == '<' ||
		ch == '>' || ch == '&' || ch == '|' || ch == '^' ||
		ch == '~' || ch == ':'
}

// isTwoCharOperator checks if a two-character string is an operator.
func isTwoCharOperator(s string) bool {
	twoCharOps := map[string]bool{
		"++": true, "--": true, "==": true, "!=": true,
		"<=": true, ">=": true, "&&": true, "||": true,
		"+=": true, "-=": true, "*=": true, "/=": true,
		"->": true, "=>": true, "::": true, "<<": true,
		">>": true,
	}
	return twoCharOps[s]
}

// isPunctuation checks if a character is punctuation.
func isPunctuation(ch rune) bool {
	return ch == '(' || ch == ')' || ch == '[' || ch == ']' ||
		ch == '{' || ch == '}' || ch == ',' || ch == ';'
}

// isIdentifier checks if a token is an identifier.
func isIdentifier(token string) bool {
	if len(token) == 0 {
		return false
	}
	first := rune(token[0])
	return unicode.IsLetter(first) || first == '_'
}

// isLiteral checks if a token is a literal value.
func isLiteral(token string) bool {
	if len(token) == 0 {
		return false
	}
	// Check for number
	first := rune(token[0])
	if unicode.IsDigit(first) {
		return true
	}
	// Check for string literal
	if first == '"' || first == '\'' || first == '`' {
		return true
	}
	// Check for boolean/nil/null
	literals := map[string]bool{
		"true": true, "false": true, "nil": true, "null": true,
		"undefined": true, "None": true, "True": true, "False": true,
	}
	return literals[token]
}

// log2 calculates base-2 logarithm.
func log2(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// log2(x) = ln(x) / ln(2)
	const ln2 = 0.6931471805599453
	return naturalLog(x) / ln2
}

// naturalLog calculates natural logarithm using Newton-Raphson.
func naturalLog(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x == 1 {
		return 0
	}
	
	// Simple approximation for ln
	// Using ln(x) = ln(x/e) + 1
	result := 0.0
	for x > 2.718281828 {
		x /= 2.718281828
		result++
	}
	
	// Taylor series approximation for ln(1+z)
	z := x - 1
	for i := 1; i <= 20; i++ {
		sign := 1.0
		if i%2 == 0 {
			sign = -1.0
		}
		result += sign * pow(z, float64(i)) / float64(i)
	}
	
	return result
}

// pow calculates x^n for integer n.
func pow(x, n float64) float64 {
	result := 1.0
	for i := 0; i < int(n); i++ {
		result *= x
	}
	return result
}