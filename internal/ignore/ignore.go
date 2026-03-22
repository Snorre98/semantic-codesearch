package ignore

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

var alwaysIgnore = []string{
	".git/",
	"node_modules/",
	"__pycache__/",
	".venv/",
	"venv/",
	".env",
	"*.pyc",
	"*.pyo",
	"*.so",
	"*.dylib",
	"*.dll",
	"*.exe",
	"*.lock",
	"package-lock.json",
	"yarn.lock",
}

var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".ico": true, ".webp": true,
	".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true,
	".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true, ".flv": true,
	".o": true, ".a": true, ".lib": true, ".class": true, ".jar": true,
	".sqlite": true, ".db": true,
}

// IsBinary returns true if the file has a known binary extension.
func IsBinary(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return binaryExtensions[ext]
}

// BuildIgnoreSpec collects .gitignore patterns from the directory tree
// and combines them with the built-in always-ignore list.
func BuildIgnoreSpec(directory string) *gitignore.GitIgnore {
	patterns := make([]string, len(alwaysIgnore))
	copy(patterns, alwaysIgnore)

	filepath.WalkDir(directory, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Name() == ".gitignore" {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					patterns = append(patterns, line)
				}
			}
		}
		return nil
	})

	return gitignore.CompileIgnoreLines(patterns...)
}

// ShouldIgnore checks whether a path should be ignored relative to baseDir.
func ShouldIgnore(path, baseDir string, spec *gitignore.GitIgnore) bool {
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		return false
	}
	return spec.MatchesPath(rel)
}
