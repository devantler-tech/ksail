package chat

//go:generate sh -c "rm -rf docs && cp -r ../../../docs/src/content/docs docs"

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed all:docs
var docsFS embed.FS

// embeddedDocumentation is built at init time from the embedded docs.
//
//nolint:gochecknoglobals // Required for go:embed pattern - must be package-level.
var embeddedDocumentation string

//nolint:gochecknoinits // Required to initialize embedded docs at startup.
func init() {
	embeddedDocumentation = buildEmbeddedDocumentation()
}

// buildEmbeddedDocumentation reads all embedded doc files and combines them.
func buildEmbeddedDocumentation() string {
	var builder strings.Builder

	// Priority files to load first
	priorityFiles := []string{
		"docs/concepts.mdx",
		"docs/features.mdx",
		"docs/use-cases.mdx",
		"docs/installation.mdx",
		"docs/support-matrix.mdx",
		"docs/troubleshooting.md",
		"docs/faq.md",
		"docs/configuration/declarative-configuration.mdx",
	}

	loadedFiles := make(map[string]bool)

	// Load priority files first
	for _, relPath := range priorityFiles {
		content, err := readEmbeddedDocFile(relPath)
		if err == nil && content != "" {
			builder.WriteString("\n## ")
			builder.WriteString(extractTitleFromPath(relPath))
			builder.WriteString("\n\n")
			builder.WriteString(content)
			builder.WriteString("\n")

			loadedFiles[relPath] = true
		}
	}

	// Load CLI flags documentation
	builder.WriteString("\n## CLI Command Reference\n\n")

	_ = fs.WalkDir(docsFS, "docs/cli-flags", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || loadedFiles[path] {
			return nil
		}

		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx") {
			return nil
		}
		// Skip index files
		if strings.HasSuffix(path, "index.mdx") || strings.HasSuffix(path, "index.md") {
			return nil
		}

		content, readErr := readEmbeddedDocFile(path)
		if readErr == nil && content != "" {
			builder.WriteString("\n### ")
			builder.WriteString(extractTitleFromPath(path))
			builder.WriteString("\n\n")
			builder.WriteString(content)
			builder.WriteString("\n")

			loadedFiles[path] = true
		}

		return nil
	})

	// Load any remaining configuration files
	_ = fs.WalkDir(docsFS, "docs/configuration", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || loadedFiles[path] {
			return nil
		}

		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx") {
			return nil
		}

		if strings.HasSuffix(path, "index.mdx") || strings.HasSuffix(path, "index.md") {
			return nil
		}

		content, readErr := readEmbeddedDocFile(path)
		if readErr == nil && content != "" {
			builder.WriteString("\n### ")
			builder.WriteString(extractTitleFromPath(path))
			builder.WriteString("\n\n")
			builder.WriteString(content)
			builder.WriteString("\n")

			loadedFiles[path] = true
		}

		return nil
	})

	return builder.String()
}

// readEmbeddedDocFile reads a doc file from the embedded FS and strips frontmatter.
func readEmbeddedDocFile(path string) (string, error) {
	content, err := docsFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading embedded doc %s: %w", path, err)
	}

	text := string(content)

	// Strip YAML frontmatter (between --- markers)
	frontmatterRegex := regexp.MustCompile(`(?s)^---\n.*?\n---\n*`)
	text = frontmatterRegex.ReplaceAllString(text, "")

	// Strip import statements
	importRegex := regexp.MustCompile(`(?m)^import\s+.*$\n*`)
	text = importRegex.ReplaceAllString(text, "")

	// Strip JSX/MDX components but keep their text content
	componentRegex := regexp.MustCompile(`<[A-Z][^>]*>|</[A-Z][^>]*>`)
	text = componentRegex.ReplaceAllString(text, "")

	return strings.TrimSpace(text), nil
}

// extractTitleFromPath extracts a readable title from a file path.
func extractTitleFromPath(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(strings.TrimSuffix(base, ".mdx"), ".md")

	if name == "index" {
		dir := filepath.Dir(path)
		name = filepath.Base(dir)
	}

	// Convert kebab-case to Title Case
	words := strings.Split(name, "-")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + word[1:]
		}
	}

	return strings.Join(words, " ")
}
