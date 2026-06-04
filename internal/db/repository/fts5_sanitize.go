// Package repository provides data access layer.
package repository

import (
	"strings"
	"unicode"
)

// sanitizeFTS5Query converts an arbitrary user/LLM-supplied query string into a
// safe FTS5 MATCH expression.
//
// FTS5 has a small but unforgiving query language: characters such as
// '"', '(', ')', '*', ':', '-', '+', '=', '^', '~', ',', '.' and operators
// like AND/OR/NOT (uppercased) carry special meaning. Free-form input from
// LLM-generated tool calls frequently contains them (e.g. `name = "Foo"`),
// which causes errors like:
//
//	SQL logic error: fts5: syntax error near "="
//
// This helper:
//  1. Splits the input into "tokens" on whitespace AND on FTS5 special
//     characters (the special characters themselves are dropped).
//  2. Wraps each non-empty token in double quotes ("phrase" form), with any
//     internal double quotes escaped per FTS5 rules ("" inside "...").
//  3. Joins phrases with a single space — FTS5 treats space-separated phrases
//     as an implicit AND, which is the most common user expectation.
//
// If the cleaned query has no usable tokens, an empty string is returned and
// the caller should short-circuit (no rows match).
func sanitizeFTS5Query(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}

	// Tokenize: any rune that is not a letter, digit, underscore, or non-ASCII
	// word char becomes a separator. This drops FTS5 specials entirely.
	tokens := strings.FieldsFunc(q, func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
		// Allow underscore (common in identifiers).
		if r == '_' {
			return false
		}
		return true
	})

	if len(tokens) == 0 {
		return ""
	}

	phrases := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		// Filter out FTS5 boolean keywords as bare tokens — when written
		// uppercase they act as operators. Lowercasing avoids that and also
		// gives consistent BM25 ranking, since FTS5 default tokenizer is
		// case-insensitive anyway.
		lowered := strings.ToLower(tok)
		// Skip pure-punctuation residue (shouldn't happen after FieldsFunc,
		// but defensive) and very short noise tokens.
		if lowered == "" {
			continue
		}
		// Escape any embedded double quotes per FTS5 phrase syntax: "" inside
		// quoted phrase. (Our tokenizer already strips '"', but this is cheap
		// and future-proof.)
		escaped := strings.ReplaceAll(lowered, `"`, `""`)
		phrases = append(phrases, `"`+escaped+`"`)
	}

	if len(phrases) == 0 {
		return ""
	}
	return strings.Join(phrases, " ")
}