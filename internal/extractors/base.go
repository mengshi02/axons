// Package extractor provides source code symbol extraction utilities.
package extractors

import (
	"fmt"

	"github.com/odvcencio/gotreesitter"
	"github.com/mengshi02/axons/pkg/types"
)

// BaseExtractor provides common functionality for all extractors.
type BaseExtractor struct {
	Language string
}

// NewExtractorOutput creates a new ExtractorOutput.
func NewExtractorOutput() *types.ExtractorOutput {
	return &types.ExtractorOutput{
		Definitions: make([]types.Definition, 0),
		Calls:       make([]types.Call, 0),
		Imports:     make([]types.Import, 0),
		Classes:     make([]types.ClassRelation, 0),
		Exports:     make([]types.Export, 0),
		TypeMap:     make(map[string]types.TypeMapEntry),
	}
}

// extractWithLanguage is a helper function that parses source code with a given language
// and extracts nodes using the provided extractor function.
func extractWithLanguage(source []byte, lang *gotreesitter.Language, extractor func(*gotreesitter.Node, []byte, *gotreesitter.Language, *types.ExtractorOutput)) (*types.ExtractorOutput, error) {
	output := NewExtractorOutput()

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("failed to parse source: %w", err)
	}
	defer tree.Release()

	root := tree.RootNode()
	if root == nil {
		return output, nil // Return empty output for empty or unparseable files
	}
	extractor(root, source, lang, output)

	return output, nil
}

// getChildByFieldNameContent gets the text content of a child node by field name.
func getChildByFieldNameContent(node *gotreesitter.Node, fieldName string, source []byte, lang *gotreesitter.Language) string {
	child := node.ChildByFieldName(fieldName, lang)
	if child != nil {
		return child.Text(source)
	}
	return ""
}