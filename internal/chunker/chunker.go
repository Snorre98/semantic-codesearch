package chunker

import (
	"fmt"
	"path/filepath"
	"strings"

	"semantic-codesearch/internal/models"
)

const (
	ChunkSize     = 60
	ChunkOverlap  = 10
	MaxChunkLines = 100
)

// DetectLanguage returns the language name for a file path, or empty string if unknown.
func DetectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	return languageMap[ext]
}

// ChunkFile splits file content into searchable code chunks.
func ChunkFile(content, filePath string) []models.CodeChunk {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	lang := DetectLanguage(filePath)

	// Tier 1: Tree-sitter AST parsing for supported languages
	if _, ok := langConfigs[lang]; ok {
		if chunks := chunkTreeSitter(content, lang); len(chunks) > 0 {
			return chunks
		}
	}

	// Tier 2: Fixed-size fallback
	return chunkFixedSize(content)
}

// FormatChunkForEmbedding adds a context header before the chunk content.
func FormatChunkForEmbedding(chunk models.CodeChunk, filePath string) string {
	lang := DetectLanguage(filePath)
	if lang == "" {
		lang = "unknown"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# File: %s\n", filePath)
	fmt.Fprintf(&b, "# Language: %s\n", lang)
	if chunk.SymbolName != "" {
		fmt.Fprintf(&b, "# Symbol: %s (%s)\n", chunk.SymbolName, chunk.ChunkType)
	}
	fmt.Fprintf(&b, "# Lines: %d-%d\n", chunk.StartLine, chunk.EndLine)
	fmt.Fprintf(&b, "\n%s", chunk.Content)
	return b.String()
}

func chunkFixedSize(content string) []models.CodeChunk {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return nil
	}

	var chunks []models.CodeChunk
	start := 0
	for start < len(lines) {
		end := start + ChunkSize
		if end > len(lines) {
			end = len(lines)
		}

		text := strings.Join(lines[start:end], "\n")
		chunks = append(chunks, models.CodeChunk{
			Content:   text,
			StartLine: start + 1,
			EndLine:   end,
			ChunkType: "block",
		})

		start += ChunkSize - ChunkOverlap
		if start >= len(lines) {
			break
		}
	}

	return chunks
}

func subChunk(text string, startLine int, chunkType, symbolName string) []models.CodeChunk {
	lines := strings.Split(text, "\n")
	var chunks []models.CodeChunk
	start := 0
	for start < len(lines) {
		end := start + ChunkSize
		if end > len(lines) {
			end = len(lines)
		}

		subText := strings.Join(lines[start:end], "\n")
		chunks = append(chunks, models.CodeChunk{
			Content:    subText,
			StartLine:  startLine + start,
			EndLine:    startLine + end - 1,
			ChunkType:  chunkType,
			SymbolName: symbolName,
		})

		start += ChunkSize - ChunkOverlap
		if start >= len(lines) {
			break
		}
	}

	return chunks
}

func joinLines(lines []string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}
