package chunker

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/swift"
	tsTS "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// languageMap maps file extensions to language identifier strings.
var languageMap = map[string]string{
	".py": "python",
	".js": "javascript", ".jsx": "javascript",
	".ts": "typescript", ".tsx": "typescript",
	".java": "java",
	".go":   "go",
	".rs":   "rust",
	".c": "c", ".h": "c",
	".cpp": "cpp", ".hpp": "cpp", ".cc": "cpp",
	".rb":  "ruby",
	".php": "php",
	".swift": "swift",
	".kt":   "kotlin",
	".scala": "scala",
	".cs":    "csharp",
	".sh": "shell", ".bash": "shell", ".zsh": "shell",
	".sql":  "sql",
	".md":   "markdown",
	".yaml": "yaml", ".yml": "yaml",
	".json": "json",
	".toml": "toml",
	".xml":  "xml",
	".html": "html", ".htm": "html",
	".css": "css", ".scss": "scss", ".less": "less",
}

// LangConfig holds tree-sitter configuration for a language.
type LangConfig struct {
	Language       *sitter.Language
	TopLevelTypes  []string
	ContainerTypes []string
	CommentTypes   []string
	ChunkTypeMap   map[string]string
	SymbolNameFunc func(node *sitter.Node, source []byte) string
}

// langConfigs maps language identifier strings to their tree-sitter configuration.
var langConfigs = map[string]*LangConfig{
	"go": {
		Language:      golang.GetLanguage(),
		TopLevelTypes: []string{"function_declaration", "method_declaration", "type_declaration"},
		CommentTypes:  []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_declaration": "function",
			"method_declaration":   "method",
			"type_declaration":     "class",
		},
		SymbolNameFunc: goSymbolName,
	},
	"python": {
		Language:       python.GetLanguage(),
		TopLevelTypes:  []string{"function_definition", "class_definition", "decorated_definition"},
		ContainerTypes: []string{"class_definition"},
		CommentTypes:   []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_definition":  "function",
			"class_definition":     "class",
			"decorated_definition": "function",
		},
		SymbolNameFunc: pythonSymbolName,
	},
	"javascript": {
		Language:       javascript.GetLanguage(),
		TopLevelTypes:  []string{"function_declaration", "class_declaration", "export_statement", "lexical_declaration"},
		ContainerTypes: []string{"class_declaration"},
		CommentTypes:   []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_declaration": "function",
			"class_declaration":    "class",
			"export_statement":     "function",
			"lexical_declaration":  "function",
		},
		SymbolNameFunc: jsSymbolName,
	},
	"typescript": {
		Language:       tsTS.GetLanguage(),
		TopLevelTypes:  []string{"function_declaration", "class_declaration", "interface_declaration", "type_alias_declaration", "export_statement", "lexical_declaration"},
		ContainerTypes: []string{"class_declaration"},
		CommentTypes:   []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_declaration":  "function",
			"class_declaration":     "class",
			"interface_declaration": "class",
			"type_alias_declaration": "class",
			"export_statement":      "function",
			"lexical_declaration":   "function",
		},
		SymbolNameFunc: jsSymbolName,
	},
	"rust": {
		Language:       rust.GetLanguage(),
		TopLevelTypes:  []string{"function_item", "impl_item", "struct_item", "enum_item", "trait_item"},
		ContainerTypes: []string{"impl_item"},
		CommentTypes:   []string{"line_comment", "block_comment"},
		ChunkTypeMap: map[string]string{
			"function_item": "function",
			"impl_item":     "class",
			"struct_item":   "class",
			"enum_item":     "class",
			"trait_item":    "class",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
	"java": {
		Language:       java.GetLanguage(),
		TopLevelTypes:  []string{"class_declaration", "interface_declaration", "enum_declaration", "method_declaration"},
		ContainerTypes: []string{"class_declaration", "interface_declaration"},
		CommentTypes:   []string{"line_comment", "block_comment"},
		ChunkTypeMap: map[string]string{
			"class_declaration":     "class",
			"interface_declaration": "class",
			"enum_declaration":      "class",
			"method_declaration":    "function",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
	"c": {
		Language:      c.GetLanguage(),
		TopLevelTypes: []string{"function_definition", "struct_specifier", "enum_specifier"},
		CommentTypes:  []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_definition": "function",
			"struct_specifier":    "class",
			"enum_specifier":     "class",
		},
		SymbolNameFunc: cSymbolName,
	},
	"cpp": {
		Language:       cpp.GetLanguage(),
		TopLevelTypes:  []string{"function_definition", "class_specifier", "struct_specifier", "enum_specifier"},
		ContainerTypes: []string{"class_specifier"},
		CommentTypes:   []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_definition": "function",
			"class_specifier":    "class",
			"struct_specifier":   "class",
			"enum_specifier":     "class",
		},
		SymbolNameFunc: cSymbolName,
	},
	"ruby": {
		Language:       ruby.GetLanguage(),
		TopLevelTypes:  []string{"method", "class", "module", "singleton_method"},
		ContainerTypes: []string{"class", "module"},
		CommentTypes:   []string{"comment"},
		ChunkTypeMap: map[string]string{
			"method":           "function",
			"singleton_method": "function",
			"class":            "class",
			"module":           "class",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
	"php": {
		Language:       php.GetLanguage(),
		TopLevelTypes:  []string{"function_definition", "class_declaration", "method_declaration"},
		ContainerTypes: []string{"class_declaration"},
		CommentTypes:   []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_definition": "function",
			"class_declaration":   "class",
			"method_declaration":  "function",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
	"csharp": {
		Language:       csharp.GetLanguage(),
		TopLevelTypes:  []string{"class_declaration", "interface_declaration", "struct_declaration", "method_declaration", "enum_declaration"},
		ContainerTypes: []string{"class_declaration", "interface_declaration"},
		CommentTypes:   []string{"comment"},
		ChunkTypeMap: map[string]string{
			"class_declaration":     "class",
			"interface_declaration": "class",
			"struct_declaration":    "class",
			"method_declaration":    "function",
			"enum_declaration":      "class",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
	"swift": {
		Language:       swift.GetLanguage(),
		TopLevelTypes:  []string{"function_declaration", "class_declaration", "struct_declaration", "protocol_declaration", "enum_declaration"},
		ContainerTypes: []string{"class_declaration"},
		CommentTypes:   []string{"comment", "multiline_comment"},
		ChunkTypeMap: map[string]string{
			"function_declaration":  "function",
			"class_declaration":     "class",
			"struct_declaration":    "class",
			"protocol_declaration":  "class",
			"enum_declaration":      "class",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
	"kotlin": {
		Language:       kotlin.GetLanguage(),
		TopLevelTypes:  []string{"function_declaration", "class_declaration", "object_declaration"},
		ContainerTypes: []string{"class_declaration"},
		CommentTypes:   []string{"line_comment", "multiline_comment"},
		ChunkTypeMap: map[string]string{
			"function_declaration": "function",
			"class_declaration":    "class",
			"object_declaration":   "class",
		},
		SymbolNameFunc: kotlinSymbolName,
	},
	"scala": {
		Language:       scala.GetLanguage(),
		TopLevelTypes:  []string{"function_definition", "class_definition", "trait_definition", "object_definition"},
		ContainerTypes: []string{"class_definition", "trait_definition"},
		CommentTypes:   []string{"comment", "block_comment"},
		ChunkTypeMap: map[string]string{
			"function_definition": "function",
			"class_definition":    "class",
			"trait_definition":    "class",
			"object_definition":   "class",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
	"shell": {
		Language:      bash.GetLanguage(),
		TopLevelTypes: []string{"function_definition"},
		CommentTypes:  []string{"comment"},
		ChunkTypeMap: map[string]string{
			"function_definition": "function",
		},
		SymbolNameFunc: nameFieldSymbol,
	},
}

// nameFieldSymbol extracts the symbol name from a node's "name" field child.
// Works for most languages where the declaration has a direct "name" child.
func nameFieldSymbol(node *sitter.Node, source []byte) string {
	n := node.ChildByFieldName("name")
	if n != nil {
		return n.Content(source)
	}
	return ""
}

// goSymbolName extracts the symbol name for Go declarations.
// For method_declaration: "ReceiverType.MethodName"
// For function_declaration: just the name
// For type_declaration: the type spec name
func goSymbolName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "function_declaration":
		return nameFieldSymbol(node, source)
	case "method_declaration":
		name := nameFieldSymbol(node, source)
		recv := node.ChildByFieldName("receiver")
		if recv != nil {
			// Walk through the parameter list to find the type
			for i := 0; i < int(recv.NamedChildCount()); i++ {
				param := recv.NamedChild(i)
				typNode := param.ChildByFieldName("type")
				if typNode != nil {
					typeName := extractGoTypeName(typNode, source)
					if typeName != "" {
						return typeName + "." + name
					}
				}
			}
		}
		return name
	case "type_declaration":
		// type_declaration contains type_spec children
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "type_spec" {
				return nameFieldSymbol(child, source)
			}
		}
	}
	return ""
}

// extractGoTypeName gets the base type name from a Go type expression,
// handling pointer types (*T), generic types (T[U]), etc.
func extractGoTypeName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "pointer_type":
		inner := node.NamedChild(0)
		if inner != nil {
			return extractGoTypeName(inner, source)
		}
	case "type_identifier":
		return node.Content(source)
	case "generic_type":
		n := node.ChildByFieldName("type")
		if n != nil {
			return extractGoTypeName(n, source)
		}
	}
	return node.Content(source)
}

// pythonSymbolName handles Python's decorated_definition by descending
// into the inner function_definition or class_definition.
func pythonSymbolName(node *sitter.Node, source []byte) string {
	if node.Type() == "decorated_definition" {
		// The actual definition is the last named child
		for i := int(node.NamedChildCount()) - 1; i >= 0; i-- {
			child := node.NamedChild(i)
			if child.Type() == "function_definition" || child.Type() == "class_definition" {
				return nameFieldSymbol(child, source)
			}
		}
	}
	return nameFieldSymbol(node, source)
}

// jsSymbolName handles JavaScript/TypeScript declarations including
// export statements and lexical declarations (const x = () => {}).
func jsSymbolName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "export_statement":
		// Descend into the exported declaration
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			switch child.Type() {
			case "function_declaration", "class_declaration", "interface_declaration",
				"type_alias_declaration", "lexical_declaration":
				return jsSymbolName(child, source)
			}
		}
		return ""
	case "lexical_declaration":
		// const foo = ...
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "variable_declarator" {
				return nameFieldSymbol(child, source)
			}
		}
		return ""
	default:
		return nameFieldSymbol(node, source)
	}
}

// cSymbolName extracts names from C/C++ declarations.
// function_definition has a "declarator" field; structs have a "name" field.
func cSymbolName(node *sitter.Node, source []byte) string {
	// Try "name" first (struct/enum/class)
	if n := node.ChildByFieldName("name"); n != nil {
		return n.Content(source)
	}
	// For function_definition, the name is inside the declarator
	decl := node.ChildByFieldName("declarator")
	if decl != nil {
		return extractDeclaratorName(decl, source)
	}
	return ""
}

// extractDeclaratorName recursively extracts the identifier from a C/C++ declarator.
func extractDeclaratorName(node *sitter.Node, source []byte) string {
	switch node.Type() {
	case "identifier", "field_identifier":
		return node.Content(source)
	case "function_declarator", "pointer_declarator", "reference_declarator":
		inner := node.ChildByFieldName("declarator")
		if inner != nil {
			return extractDeclaratorName(inner, source)
		}
		// fallback: first named child
		if node.NamedChildCount() > 0 {
			return extractDeclaratorName(node.NamedChild(0), source)
		}
	}
	return ""
}

// kotlinSymbolName extracts names from Kotlin declarations.
// Kotlin's tree-sitter grammar uses "simple_identifier" children for names.
func kotlinSymbolName(node *sitter.Node, source []byte) string {
	// Try standard "name" field first
	if n := node.ChildByFieldName("name"); n != nil {
		return n.Content(source)
	}
	// Kotlin often uses simple_identifier as a direct child
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() == "simple_identifier" {
			return child.Content(source)
		}
	}
	return ""
}
