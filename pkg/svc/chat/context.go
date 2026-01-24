package chat

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"
)

// BuildSystemContext builds the system prompt context for the chat assistant.
// It combines multiple sources of information:
//   - Identity and role description for the AI assistant
//   - Current working directory and ksail.yaml configuration if present
//   - KSail documentation loaded from the docs/ directory
//   - Dynamic CLI help output from the ksail executable
//   - Instructions for proper behavior and command usage
//
// Returns the complete system context string and any error encountered.
// If documentation loading fails, a partial context is still returned.
func BuildSystemContext() (string, error) {
	var builder strings.Builder

	// Add identity and role
	builder.WriteString(`<identity>
You are KSail Assistant, an AI helper for KSail - a CLI tool for creating and managing local Kubernetes clusters.
You help users configure, troubleshoot, and operate Kubernetes clusters using KSail.
</identity>

`)

	// Add working directory context
	workDir, err := os.Getwd()
	if err == nil {
		builder.WriteString(fmt.Sprintf("<working_directory>%s</working_directory>\n\n", workDir))

		// Check if ksail.yaml exists in working directory
		if _, statErr := os.Stat(filepath.Join(workDir, "ksail.yaml")); statErr == nil {
			content, readErr := os.ReadFile(filepath.Join(workDir, "ksail.yaml"))
			if readErr == nil {
				builder.WriteString("<current_ksail_config>\n")
				builder.WriteString(string(content))
				builder.WriteString("\n</current_ksail_config>\n\n")
			}
		}
	}

	// Load documentation from docs/ directory
	docs := loadDocumentation()
	if docs != "" {
		builder.WriteString("<ksail_documentation>\n")
		builder.WriteString(docs)
		builder.WriteString("\n</ksail_documentation>\n\n")
	}

	// Add CLI help dynamically
	cliHelp := getCLIHelp()
	if cliHelp != "" {
		builder.WriteString("<cli_help>\n")
		builder.WriteString(cliHelp)
		builder.WriteString("\n</cli_help>\n\n")
	}

	// Add instructions
	builder.WriteString(ksailInstructions)

	return builder.String(), nil
}

// getCLIHelp captures the ksail --help output dynamically.
func getCLIHelp() string {
	ksailPath := FindKSailExecutable()
	if ksailPath == "" {
		return ""
	}

	const cliHelpTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), cliHelpTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ksailPath, "--help")

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return string(output)
}

// loadDocumentation reads documentation from the docs/ directory.
func loadDocumentation() string {
	docsDir := findDocsDirectory()
	if docsDir == "" {
		return ""
	}

	var builder strings.Builder

	// Priority files to load first (core documentation)
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

	// Load priority files first
	for _, relPath := range priorityFiles {
		fullPath := filepath.Join(docsDir, relPath)
		if content, err := readDocFile(fullPath); err == nil {
			builder.WriteString(fmt.Sprintf("\n## %s\n\n", extractTitle(relPath)))
			builder.WriteString(content)
			builder.WriteString("\n")

			loadedFiles[fullPath] = true
		}
	}

	// Walk CLI flags directory for command reference
	cliDir := filepath.Join(docsDir, "cli-flags")

	_, statErr := os.Stat(cliDir)
	if statErr == nil {
		builder.WriteString("\n## CLI Command Reference\n\n")

		_ = filepath.WalkDir(cliDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || loadedFiles[path] {
				return nil
			}

			if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx") {
				return nil
			}

			// Skip index files in CLI flags
			if strings.HasSuffix(path, "index.mdx") || strings.HasSuffix(path, "index.md") {
				return nil
			}

			if content, readErr := readDocFile(path); readErr == nil {
				relPath, _ := filepath.Rel(docsDir, path)
				builder.WriteString(fmt.Sprintf("\n### %s\n\n", extractTitle(relPath)))
				builder.WriteString(content)
				builder.WriteString("\n")

				loadedFiles[path] = true
			}

			return nil
		})
	}

	return builder.String()
}

// findDocsDirectory locates the docs/src/content/docs directory.
func findDocsDirectory() string {
	// Check relative to executable
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)

		candidates := []string{
			filepath.Join(exeDir, "docs", "src", "content", "docs"),
			filepath.Join(exeDir, "..", "docs", "src", "content", "docs"),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate
			}
		}
	}

	// Check relative to working directory
	workDir, err := os.Getwd()
	if err == nil {
		candidates := []string{
			filepath.Join(workDir, "docs", "src", "content", "docs"),
			filepath.Join(workDir, "..", "docs", "src", "content", "docs"),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate
			}
		}
	}

	// Check common development locations
	homeDir, err := os.UserHomeDir()
	if err == nil {
		commonPaths := []string{
			filepath.Join(
				homeDir,
				"go",
				"src",
				"github.com",
				"devantler-tech",
				"ksail",
				"docs",
				"src",
				"content",
				"docs",
			),
		}
		for _, p := range commonPaths {
			if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
				return p
			}
		}
	}

	return ""
}

// readDocFile reads a markdown/mdx file and strips frontmatter.
func readDocFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading doc file: %w", err)
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

// extractTitle extracts a readable title from a file path.
func extractTitle(path string) string {
	// Get filename without extension
	base := filepath.Base(path)
	name := strings.TrimSuffix(strings.TrimSuffix(base, ".mdx"), ".md")

	// Handle special cases
	if name == "index" {
		// Use parent directory name
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

// FindKSailExecutable attempts to find the ksail executable.
func FindKSailExecutable() string {
	// Check if running from ksail itself
	exe, err := os.Executable()
	if err == nil && strings.Contains(filepath.Base(exe), "ksail") {
		return exe
	}

	// Check PATH
	path, err := exec.LookPath("ksail")
	if err == nil {
		return path
	}

	// Check common locations on macOS/Linux
	if goruntime.GOOS != "windows" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			commonPaths := []string{
				filepath.Join(homeDir, "go", "bin", "ksail"),
				filepath.Join(homeDir, ".local", "bin", "ksail"),
				"/usr/local/bin/ksail",
			}
			for _, p := range commonPaths {
				if _, statErr := os.Stat(p); statErr == nil {
					return p
				}
			}
		}
	}

	return ""
}

// ksailInstructions contains instructions for the AI assistant.
const ksailInstructions = `<instructions>
- Help users configure and manage Kubernetes clusters using KSail
- When suggesting commands, explain what they do before running them
- For write operations (creating clusters, applying workloads, deleting resources), the user will be prompted to confirm
- Prefer KSail commands over raw kubectl when KSail provides equivalent functionality
- Reference the documentation when helping with ksail.yaml configuration
- Use the troubleshooting tips for diagnosing issues
- Be concise but thorough in explanations
- If a ksail.yaml exists in the working directory, reference it when relevant
- When generating configuration, follow the ksail.yaml schema
- For cluster operations, verify the cluster exists first with 'ksail cluster list'
- IMPORTANT: Do NOT call the same tool multiple times with the same arguments. If a command returns "No clusters found", respond to the user accordingly - do not retry the command.
- When asked to delete/modify clusters but none exist, inform the user there are no clusters to act on.
</instructions>
`
