// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the MIT License.

//go:build ignore

// gen_docs.go reads documentation from docs/src/content/docs/ and generates
// a Go source file (docs_generated.go) containing the pre-processed
// documentation as a string constant. This eliminates the need to copy and
// embed the entire docs directory in pkg/svc/chat/.
//
// Usage:
//
//	go run gen_docs.go
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	// sourceDocsDir is the relative path from this file to the source docs.
	sourceDocsDir = "../../../docs/src/content/docs"
	// outputFile is the generated Go source file.
	outputFile = "docs_generated.go"
)

func main() {
	docs := buildDocumentation()

	src := generateGoSource(docs)

	if err := os.WriteFile(outputFile, []byte(src), 0o644); err != nil {
		log.Fatalf("writing %s: %v", outputFile, err)
	}

	fmt.Printf("gen_docs: wrote %s (%d bytes of documentation)\n", outputFile, len(docs))
}

// buildDocumentation reads all doc files and combines them, mirroring the
// logic previously in buildEmbeddedDocumentation.
func buildDocumentation() string {
	var builder strings.Builder

	// Priority files to load first.
	priorityFiles := []string{
		"concepts.mdx",
		"features.mdx",
		"use-cases.mdx",
		"installation.mdx",
		"support-matrix.mdx",
		"troubleshooting.md",
		"faq.md",
		"configuration/declarative-configuration.mdx",
	}

	loadedFiles := make(map[string]bool)

	// Load priority files first.
	for _, relPath := range priorityFiles {
		fullPath := filepath.Join(sourceDocsDir, relPath)

		content, err := readDocFile(fullPath)
		if err != nil || content == "" {
			continue
		}

		builder.WriteString("\n## ")
		builder.WriteString(extractTitleFromPath(relPath))
		builder.WriteString("\n\n")
		builder.WriteString(content)
		builder.WriteString("\n")

		loadedFiles[relPath] = true
	}

	// Load CLI flags documentation.
	builder.WriteString("\n## CLI Command Reference\n\n")
	processDocDirectory(&builder, filepath.Join(sourceDocsDir, "cli-flags"), "### ", loadedFiles)

	// Load any remaining configuration files.
	processDocDirectory(
		&builder,
		filepath.Join(sourceDocsDir, "configuration"),
		"### ",
		loadedFiles,
	)

	return builder.String()
}

// processDocDirectory walks a directory and processes all doc files.
func processDocDirectory(
	builder *strings.Builder,
	dir, titlePrefix string,
	loadedFiles map[string]bool,
) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Compute the relative path from sourceDocsDir for dedup tracking.
		relPath, relErr := filepath.Rel(sourceDocsDir, path)
		if relErr != nil {
			return nil
		}

		if loadedFiles[relPath] {
			return nil
		}

		if !isDocFile(path) || isIndexFile(path) {
			return nil
		}

		content, readErr := readDocFile(path)
		if readErr != nil || content == "" {
			return nil
		}

		builder.WriteString("\n")
		builder.WriteString(titlePrefix)
		builder.WriteString(extractTitleFromPath(relPath))
		builder.WriteString("\n\n")
		builder.WriteString(content)
		builder.WriteString("\n")

		loadedFiles[relPath] = true

		return nil
	})
	if err != nil {
		log.Printf("warning: walking %s: %v", dir, err)
	}
}

// isDocFile checks if a path is a markdown/mdx documentation file.
func isDocFile(path string) bool {
	return strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".mdx")
}

// isIndexFile checks if a path is an index file.
func isIndexFile(path string) bool {
	return strings.HasSuffix(path, "index.mdx") || strings.HasSuffix(path, "index.md")
}

// readDocFile reads a doc file from disk and strips frontmatter/MDX syntax.
func readDocFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading doc %s: %w", path, err)
	}

	text := string(content)

	// Strip YAML frontmatter (between --- markers).
	frontmatterRegex := regexp.MustCompile(`(?s)^---\n.*?\n---\n*`)
	text = frontmatterRegex.ReplaceAllString(text, "")

	// Strip import statements.
	importRegex := regexp.MustCompile(`(?m)^import\s+.*$\n*`)
	text = importRegex.ReplaceAllString(text, "")

	// Strip JSX/MDX components but keep their text content.
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

	// Convert kebab-case to Title Case.
	words := strings.Split(name, "-")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(string(word[0])) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// generateGoSource produces a valid Go source file with the documentation
// as a string constant.
func generateGoSource(docs string) string {
	var b strings.Builder

	b.WriteString("// Code generated by gen_docs.go; DO NOT EDIT.\n\n")
	b.WriteString("package chat\n\n")
	b.WriteString("// generatedDocumentation contains the pre-processed KSail documentation,\n")
	b.WriteString("// built at go:generate time from docs/src/content/docs/.\n")
	b.WriteString("const generatedDocumentation = ")
	b.WriteString(strconv.Quote(docs))
	b.WriteString("\n")

	return b.String()
}
