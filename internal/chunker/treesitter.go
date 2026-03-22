package chunker

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"semantic-codesearch/internal/models"
)

// chunkTreeSitter parses the file content using tree-sitter and extracts
// semantic chunks (functions, classes, etc.) based on the language config.
// Returns nil if parsing fails or no chunks are found (triggers fallback).
func chunkTreeSitter(content, lang string) []models.CodeChunk {
	cfg, ok := langConfigs[lang]
	if !ok || cfg == nil {
		return nil
	}

	source := []byte(content)
	parser := sitter.NewParser()
	parser.SetLanguage(cfg.Language)

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil || tree == nil {
		return nil
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil || root.NamedChildCount() == 0 {
		return nil
	}

	lines := strings.Split(content, "\n")
	var chunks []models.CodeChunk

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		extracted := extractNode(child, cfg, source, lines)
		chunks = append(chunks, extracted...)
	}

	return chunks
}

// extractNode processes a single AST node and returns chunks for it.
func extractNode(node *sitter.Node, cfg *LangConfig, source []byte, lines []string) []models.CodeChunk {
	nodeType := node.Type()

	if !isInList(nodeType, cfg.TopLevelTypes) {
		return nil
	}

	// Determine chunk type
	chunkType := cfg.ChunkTypeMap[nodeType]
	if chunkType == "" {
		chunkType = "block"
	}

	// For Python decorated_definition, resolve the actual type from inner definition
	if nodeType == "decorated_definition" {
		inner := findInnerDefinition(node)
		if inner != nil {
			if ct, ok := cfg.ChunkTypeMap[inner.Type()]; ok {
				chunkType = ct
			}
		}
	}

	// For JS/TS export_statement, resolve the actual type from inner declaration
	if nodeType == "export_statement" {
		inner := findExportedDeclaration(node)
		if inner != nil {
			if ct, ok := cfg.ChunkTypeMap[inner.Type()]; ok {
				chunkType = ct
			}
		}
	}

	// Extract symbol name
	symbolName := ""
	if cfg.SymbolNameFunc != nil {
		symbolName = cfg.SymbolNameFunc(node, source)
	}

	// Capture leading comments
	commentStartRow := captureLeadingComments(node, cfg, source)
	nodeStartRow := node.StartPoint().Row
	nodeEndRow := node.EndPoint().Row

	startRow := nodeStartRow
	if commentStartRow < startRow {
		startRow = commentStartRow
	}

	// Convert to 1-indexed line numbers
	startLine := int(startRow) + 1
	endLine := int(nodeEndRow) + 1
	lineCount := endLine - startLine + 1

	// Extract text
	text := joinLines(lines, int(startRow), int(nodeEndRow)+1)

	// Container handling: if it's a container type and too large, extract children
	if isInList(nodeType, cfg.ContainerTypes) && lineCount > MaxChunkLines {
		return extractContainerChildren(node, cfg, source, lines, commentStartRow, chunkType, symbolName)
	}

	// Sub-chunk if too large
	if lineCount > MaxChunkLines {
		return subChunk(text, startLine, chunkType, symbolName)
	}

	return []models.CodeChunk{{
		Content:    text,
		StartLine:  startLine,
		EndLine:    endLine,
		ChunkType:  chunkType,
		SymbolName: symbolName,
	}}
}

// extractContainerChildren breaks a large container (class, impl, module) into
// individual method/function chunks plus a signature chunk for the container header.
func extractContainerChildren(container *sitter.Node, cfg *LangConfig, source []byte, lines []string, commentStartRow uint32, containerType, containerName string) []models.CodeChunk {
	var chunks []models.CodeChunk

	// Method-like node types within containers
	methodTypes := map[string]bool{
		"function_definition":  true,
		"function_declaration": true,
		"method_declaration":   true,
		"method":               true,
		"singleton_method":     true,
		"function_item":        true,
	}

	// Collect ranges of child methods to find the header
	var childChunks []models.CodeChunk
	childRanges := make(map[int]bool) // line numbers covered by children

	for i := 0; i < int(container.NamedChildCount()); i++ {
		child := container.NamedChild(i)
		if !methodTypes[child.Type()] {
			continue
		}

		childCommentStart := captureLeadingComments(child, cfg, source)
		childStartRow := child.StartPoint().Row
		if childCommentStart < childStartRow {
			childStartRow = childCommentStart
		}
		childEndRow := child.EndPoint().Row

		childStartLine := int(childStartRow) + 1
		childEndLine := int(childEndRow) + 1
		childLineCount := childEndLine - childStartLine + 1

		childSymbol := ""
		if cfg.SymbolNameFunc != nil {
			childSymbol = cfg.SymbolNameFunc(child, source)
		}
		// Prefix with container name for context
		if containerName != "" && childSymbol != "" {
			childSymbol = containerName + "." + childSymbol
		}

		childText := joinLines(lines, int(childStartRow), int(childEndRow)+1)

		for ln := childStartLine; ln <= childEndLine; ln++ {
			childRanges[ln] = true
		}

		if childLineCount > MaxChunkLines {
			childChunks = append(childChunks, subChunk(childText, childStartLine, "method", childSymbol)...)
		} else {
			childChunks = append(childChunks, models.CodeChunk{
				Content:    childText,
				StartLine:  childStartLine,
				EndLine:    childEndLine,
				ChunkType:  "method",
				SymbolName: childSymbol,
			})
		}
	}

	// If no methods found, just sub-chunk the whole container
	if len(childChunks) == 0 {
		containerStartLine := int(commentStartRow) + 1
		text := joinLines(lines, int(commentStartRow), int(container.EndPoint().Row)+1)
		return subChunk(text, containerStartLine, containerType, containerName)
	}

	// Build a signature chunk from lines not covered by methods
	containerStartLine := int(commentStartRow) + 1
	containerEndRow := int(container.EndPoint().Row) + 1
	var sigLines []string
	for ln := containerStartLine; ln <= containerEndRow; ln++ {
		if !childRanges[ln] && ln-1 < len(lines) {
			sigLines = append(sigLines, lines[ln-1])
		}
	}
	if len(sigLines) > 0 {
		sigText := strings.Join(sigLines, "\n")
		chunks = append(chunks, models.CodeChunk{
			Content:    sigText,
			StartLine:  containerStartLine,
			EndLine:    containerEndRow,
			ChunkType:  containerType,
			SymbolName: containerName,
		})
	}

	chunks = append(chunks, childChunks...)
	return chunks
}

// captureLeadingComments scans backwards from a node to find contiguous
// comment nodes immediately above it. Returns the start row of the first comment,
// or the node's own start row if no comments are found.
func captureLeadingComments(node *sitter.Node, cfg *LangConfig, source []byte) uint32 {
	startRow := node.StartPoint().Row

	prev := node.PrevNamedSibling()
	for prev != nil {
		if !isInList(prev.Type(), cfg.CommentTypes) {
			break
		}
		prevEndRow := prev.EndPoint().Row
		// Allow at most 1 blank line gap between comment and node/next comment
		if startRow-prevEndRow > 2 {
			break
		}
		startRow = prev.StartPoint().Row
		prev = prev.PrevNamedSibling()
	}

	return startRow
}

// findInnerDefinition returns the actual function or class definition
// inside a Python decorated_definition node.
func findInnerDefinition(node *sitter.Node) *sitter.Node {
	for i := int(node.NamedChildCount()) - 1; i >= 0; i-- {
		child := node.NamedChild(i)
		switch child.Type() {
		case "function_definition", "class_definition":
			return child
		}
	}
	return nil
}

// findExportedDeclaration returns the actual declaration inside a
// JS/TS export_statement node.
func findExportedDeclaration(node *sitter.Node) *sitter.Node {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		switch child.Type() {
		case "function_declaration", "class_declaration", "interface_declaration",
			"type_alias_declaration", "lexical_declaration":
			return child
		}
	}
	return nil
}

// isInList checks if a string is present in a slice.
func isInList(s string, list []string) bool {
	for _, item := range list {
		if s == item {
			return true
		}
	}
	return false
}
