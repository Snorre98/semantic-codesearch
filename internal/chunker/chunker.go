package chunker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"

	"semantic-codesearch/internal/models"
)

const (
	ChunkSize     = 60
	ChunkOverlap  = 10
	MaxChunkLines = 100
)

var languageMap = map[string]string{
	".py": "python",
	".js": "javascript", ".jsx": "javascript",
	".ts": "typescript", ".tsx": "typescript",
	".java": "java",
	".go": "go",
	".rs": "rust",
	".c": "c", ".h": "c",
	".cpp": "cpp", ".hpp": "cpp", ".cc": "cpp",
	".rb": "ruby",
	".php": "php",
	".swift": "swift",
	".kt": "kotlin",
	".scala": "scala",
	".cs": "csharp",
	".sh": "shell", ".bash": "shell", ".zsh": "shell",
	".sql": "sql",
	".md": "markdown",
	".yaml": "yaml", ".yml": "yaml",
	".json": "json",
	".toml": "toml",
	".xml": "xml",
	".html": "html", ".htm": "html",
	".css": "css", ".scss": "scss", ".less": "less",
}

var boundaryPatterns = map[string][]*regexp.Regexp{
	"python": {regexp.MustCompile(`^\s*(?:async\s+)?def\s+\w+|^\s*class\s+\w+`)},
	"javascript": {regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+\w+|^(?:export\s+)?(?:const|let|var)\s+\w+\s*=\s*(?:async\s+)?\(|^class\s+\w+`)},
	"typescript": {regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+\w+|^(?:export\s+)?(?:const|let|var)\s+\w+\s*=\s*(?:async\s+)?\(|^(?:export\s+)?class\s+\w+|^(?:export\s+)?interface\s+\w+`)},
	"java":   {regexp.MustCompile(`^\s*(?:public|private|protected|static|\s)*(?:class|interface|enum)\s+\w+|^\s*(?:public|private|protected|static|\s)*\w+(?:<[^>]*>)?\s+\w+\s*\(`)},
	"go":     {regexp.MustCompile(`^func\s+|^type\s+\w+\s+(?:struct|interface)`)},
	"rust":   {regexp.MustCompile(`^(?:pub\s+)?(?:async\s+)?fn\s+|^(?:pub\s+)?(?:struct|enum|trait|impl)\s+`)},
	"ruby":   {regexp.MustCompile(`^\s*(?:def|class|module)\s+`)},
	"php":    {regexp.MustCompile(`^\s*(?:public|private|protected|static|\s)*function\s+|^\s*class\s+`)},
	"csharp": {regexp.MustCompile(`^\s*(?:public|private|protected|internal|static|\s)*(?:class|interface|struct|enum)\s+|^\s*(?:public|private|protected|internal|static|\s)*\w+\s+\w+\s*\(`)},
	"kotlin": {regexp.MustCompile(`^\s*(?:fun|class|interface|object|data class)\s+`)},
	"swift":  {regexp.MustCompile(`^\s*(?:func|class|struct|enum|protocol)\s+`)},
	"scala":  {regexp.MustCompile(`^\s*(?:def|class|trait|object|case class)\s+`)},
}

var symbolNameRe = regexp.MustCompile(`(?:function|def|fn|func|class|interface|struct|trait|enum|type|impl|object|module)\s+(\w+)`)

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

	// Tier 1: Go AST for .go files
	if lang == "go" {
		chunks := chunkGoAST(content)
		if len(chunks) > 0 {
			return chunks
		}
	}

	// Tier 2: Heuristic regex for languages with boundary patterns
	if lang != "" {
		if _, ok := boundaryPatterns[lang]; ok {
			chunks := chunkByHeuristic(content, lang)
			if len(chunks) > 0 {
				return chunks
			}
		}
	}

	// Tier 3: Fixed-size fallback
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

func chunkGoAST(content string) []models.CodeChunk {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		return nil
	}

	lines := strings.Split(content, "\n")
	var chunks []models.CodeChunk

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start := fset.Position(d.Pos()).Line
			end := fset.Position(d.End()).Line
			chunkType := "function"
			symbolName := d.Name.Name

			if d.Recv != nil && len(d.Recv.List) > 0 {
				chunkType = "method"
				recvType := receiverTypeName(d.Recv.List[0].Type)
				if recvType != "" {
					symbolName = recvType + "." + d.Name.Name
				}
			}

			text := joinLines(lines, start-1, end)
			if end-start+1 > MaxChunkLines {
				chunks = append(chunks, subChunk(text, start, chunkType, symbolName)...)
			} else {
				chunks = append(chunks, models.CodeChunk{
					Content:    text,
					StartLine:  start,
					EndLine:    end,
					ChunkType:  chunkType,
					SymbolName: symbolName,
				})
			}

		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				switch ts.Type.(type) {
				case *ast.StructType, *ast.InterfaceType:
					start := fset.Position(d.Pos()).Line
					end := fset.Position(d.End()).Line
					text := joinLines(lines, start-1, end)

					if end-start+1 > MaxChunkLines {
						chunks = append(chunks, subChunk(text, start, "class", ts.Name.Name)...)
					} else {
						chunks = append(chunks, models.CodeChunk{
							Content:    text,
							StartLine:  start,
							EndLine:    end,
							ChunkType:  "class",
							SymbolName: ts.Name.Name,
						})
					}
				}
			}
		}
	}

	return chunks
}

func chunkByHeuristic(content, language string) []models.CodeChunk {
	patterns, ok := boundaryPatterns[language]
	if !ok {
		return nil
	}

	lines := strings.Split(content, "\n")
	var boundaries []int

	for i, line := range lines {
		for _, p := range patterns {
			if p.MatchString(line) {
				boundaries = append(boundaries, i)
				break
			}
		}
	}

	if len(boundaries) == 0 {
		return nil
	}

	var chunks []models.CodeChunk
	for idx, start := range boundaries {
		end := len(lines) - 1
		if idx+1 < len(boundaries) {
			end = boundaries[idx+1] - 1
		}

		text := strings.Join(lines[start:end+1], "\n")
		firstLine := strings.TrimSpace(lines[start])

		var symbolName string
		if m := symbolNameRe.FindStringSubmatch(firstLine); len(m) > 1 {
			symbolName = m[1]
		}

		chunkType := "function"
		for _, kw := range []string{"class ", "interface ", "struct ", "trait ", "enum ", "impl "} {
			if strings.Contains(firstLine, kw) {
				chunkType = "class"
				break
			}
		}

		if end-start+1 > MaxChunkLines {
			chunks = append(chunks, subChunk(text, start+1, chunkType, symbolName)...)
		} else {
			chunks = append(chunks, models.CodeChunk{
				Content:    text,
				StartLine:  start + 1,
				EndLine:    end + 1,
				ChunkType:  chunkType,
				SymbolName: symbolName,
			})
		}
	}

	return chunks
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

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		return receiverTypeName(t.X)
	default:
		return ""
	}
}
